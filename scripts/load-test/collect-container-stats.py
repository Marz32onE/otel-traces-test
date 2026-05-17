#!/usr/bin/env python3
"""Sample CPU/memory for Go services using instrumentation-go during load tests."""

from __future__ import annotations

import argparse
import json
import re
import signal
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

GO_SERVICES: dict[str, dict[str, Any]] = {
    "api": {
        "label": "api",
        "instrumentation": ["otel-nats", "otel-mongo/v2"],
        "role": "HTTP 入口；Publish / Mongo Insert",
    },
    "worker": {
        "label": "worker",
        "instrumentation": ["otel-nats", "otel-gorilla-ws"],
        "role": "NATS 消費、WebSocket 廣播",
    },
    "dbwatcher": {
        "label": "dbwatcher",
        "instrumentation": ["otel-mongo/v2", "otel-nats"],
        "role": "Mongo change stream → NATS",
    },
}


def parse_duration_seconds(s: str) -> int:
    s = s.strip().lower()
    if s.endswith("ms"):
        return max(1, int(float(s[:-2]) / 1000))
    if s.endswith("s"):
        return max(1, int(float(s[:-1])))
    if s.endswith("m"):
        return max(1, int(float(s[:-1]) * 60))
    if s.endswith("h"):
        return max(1, int(float(s[:-1]) * 3600))
    return max(1, int(float(s)))


def parse_mem_mib(usage: str) -> float | None:
    # e.g. "45.2MiB / 7.65GiB" or "512MB / 16GB"
    part = usage.split("/")[0].strip()
    m = re.match(r"^([\d.]+)\s*([KMGTP]?i?B)$", part, re.I)
    if not m:
        return None
    val = float(m.group(1))
    unit = m.group(2).upper()
    if unit in ("B",):
        return val / (1024 * 1024)
    if unit in ("KIB", "KB"):
        return val / 1024
    if unit in ("MIB", "MB"):
        return val
    if unit in ("GIB", "GB"):
        return val * 1024
    return val


def parse_cpu_percent(s: str) -> float | None:
    s = s.strip().rstrip("%")
    try:
        return float(s)
    except ValueError:
        return None


def service_key_from_name(name: str) -> str | None:
    n = name.lower()
    for key in GO_SERVICES:
        if f"-{key}-" in n or n.endswith(f"-{key}-1"):
            return key
    return None


def compose_container_ids(root: Path, services: list[str], compose_cmd: str) -> dict[str, str]:
    ids: dict[str, str] = {}
    for svc in services:
        proc = subprocess.run(
            compose_cmd.split() + ["-f", str(root / "docker-compose.yml"), "ps", "-q", svc],
            cwd=root,
            capture_output=True,
            text=True,
            check=False,
        )
        cid = proc.stdout.strip().splitlines()[0] if proc.stdout.strip() else ""
        if cid:
            ids[svc] = cid
    return ids


def docker_stats_sample(container_ids: dict[str, str]) -> dict[str, dict[str, float]]:
    out: dict[str, dict[str, float]] = {}
    for svc, cid in container_ids.items():
        proc = subprocess.run(
            ["docker", "stats", "--no-stream", "--format", "{{json .}}", cid],
            capture_output=True,
            text=True,
            check=False,
        )
        if proc.returncode != 0 or not proc.stdout.strip():
            continue
        try:
            row = json.loads(proc.stdout.strip().splitlines()[-1])
        except json.JSONDecodeError:
            continue
        cpu = parse_cpu_percent(row.get("CPUPerc", ""))
        mem = parse_mem_mib(row.get("MemUsage", ""))
        if cpu is not None:
            out.setdefault(svc, {})["cpu"] = cpu
        if mem is not None:
            out.setdefault(svc, {})["mem_mib"] = mem
    return out


def aggregate(samples: dict[str, list[dict[str, float]]]) -> dict[str, Any]:
    services: dict[str, Any] = {}
    for svc, points in samples.items():
        meta = GO_SERVICES.get(svc, {"label": svc, "instrumentation": [], "role": ""})
        cpus = [p["cpu"] for p in points if "cpu" in p]
        mems = [p["mem_mib"] for p in points if "mem_mib" in p]
        services[svc] = {
            "label": meta["label"],
            "instrumentation": meta.get("instrumentation", []),
            "role": meta.get("role", ""),
            "samples": len(points),
            "cpu_percent": {
                "avg": round(sum(cpus) / len(cpus), 2) if cpus else None,
                "max": round(max(cpus), 2) if cpus else None,
            },
            "memory_mib": {
                "avg": round(sum(mems) / len(mems), 2) if mems else None,
                "max": round(max(mems), 2) if mems else None,
            },
        }
    return {"services": services}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True, help="repo root (docker-compose.yml)")
    parser.add_argument("--compose-cmd", default="docker compose")
    parser.add_argument("--services", default="api,worker,dbwatcher")
    parser.add_argument("--duration", required=True, help="e.g. 30s, 3m")
    parser.add_argument("--interval", type=float, default=2.0)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--warmup", type=float, default=3.0, help="seconds before sampling")
    args = parser.parse_args()

    duration_sec = parse_duration_seconds(args.duration)
    service_list = [s.strip() for s in args.services.split(",") if s.strip()]
    container_ids = compose_container_ids(args.root, service_list, args.compose_cmd)
    if not container_ids:
        print("no containers found for services", file=sys.stderr)
        return 1

    samples: dict[str, list[dict[str, float]]] = {s: [] for s in container_ids}

    stop = False

    def handle_sig(*_args: object) -> None:
        nonlocal stop
        stop = True

    signal.signal(signal.SIGTERM, handle_sig)
    signal.signal(signal.SIGINT, handle_sig)

    if args.warmup > 0:
        time.sleep(args.warmup)

    end = time.time() + duration_sec
    while time.time() < end and not stop:
        snap = docker_stats_sample(container_ids)
        for svc, vals in snap.items():
            samples.setdefault(svc, []).append(vals)
        time.sleep(args.interval)

    result = aggregate(samples)
    result["meta"] = {
        "duration_sec": duration_sec,
        "interval_sec": args.interval,
        "container_ids": container_ids,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2), encoding="utf-8")
    print(f"resources written: {args.output}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
