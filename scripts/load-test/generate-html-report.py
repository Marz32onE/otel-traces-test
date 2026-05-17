#!/usr/bin/env python3
"""Generate zh-TW HTML load test report from k6 handleSummary JSON files."""

from __future__ import annotations

import argparse
import html
import json
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

TC_META: dict[int, dict[str, str]] = {
    1: {
        "title": "TC1 — 全部關閉",
        "flags": "Instrumentation 全關；取樣 always_on (100%)",
        "traceparent": "有",
    },
    2: {
        "title": "TC2 — Trace 開、傳播關",
        "flags": "Tracing 開、Propagation 關；取樣 100%",
        "traceparent": "有",
    },
    3: {
        "title": "TC3 — Trace 開、傳播關、1% 取樣",
        "flags": "Tracing 開、Propagation 關；traceidratio 0.01",
        "traceparent": "無（由 SDK 決定 root 取樣）",
    },
    4: {
        "title": "TC4 — 全部開啟",
        "flags": "Tracing + Propagation 全開；取樣 100%",
        "traceparent": "有",
    },
    5: {
        "title": "TC5 — 全部開啟、1% 取樣",
        "flags": "Tracing + Propagation 全開；traceidratio 0.01",
        "traceparent": "無",
    },
}

GO_SERVICE_INST: dict[str, list[str]] = {
    "api": ["otel-nats", "otel-mongo/v2"],
    "worker": ["otel-nats", "otel-gorilla-ws"],
    "dbwatcher": ["otel-mongo/v2", "otel-nats"],
}


def load_summary(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as f:
        return json.load(f)


def metric_values(data: dict[str, Any], name: str) -> dict[str, float]:
    metrics = data.get("metrics") or {}
    m = metrics.get(name) or {}
    vals = m.get("values") or {}
    return {k: float(v) for k, v in vals.items() if isinstance(v, (int, float))}


def fmt_ms(k6_ms: float | None) -> str:
    """Format k6 summary trend values (already in milliseconds)."""
    if k6_ms is None:
        return "—"
    return f"{k6_ms:.2f}"


def fmt_rate(rate: float | None) -> str:
    if rate is None:
        return "—"
    return f"{rate * 100:.3f}%"


def fmt_int(n: float | None) -> str:
    if n is None:
        return "—"
    return f"{int(n):,}"


def threshold_ok(data: dict[str, Any]) -> tuple[bool, list[str]]:
    failed: list[str] = []
    metrics = data.get("metrics") or {}
    for mname, mdef in metrics.items():
        for th in (mdef.get("thresholds") or {}).values():
            if th.get("ok") is False:
                failed.append(mname)
    return len(failed) == 0, failed


def load_resources(path: Path) -> dict[str, Any] | None:
    if not path.is_file():
        return None
    with path.open(encoding="utf-8") as f:
        return json.load(f)


def fmt_cpu(v: float | None) -> str:
    if v is None:
        return "—"
    return f"{v:.1f}%"


def fmt_mem(v: float | None) -> str:
    if v is None:
        return "—"
    return f"{v:.1f} MiB"


def render_resources_table(resources: dict[str, Any] | None) -> str:
    if not resources:
        return '<p class="warn">無容器資源資料（請確認壓測時已執行 collect-container-stats）。</p>'
    services = resources.get("services") or {}
    if not services:
        return '<p class="warn">資源 JSON 為空。</p>'
    rows = []
    for svc in ("api", "worker", "dbwatcher"):
        s = services.get(svc)
        if not s:
            continue
        inst = html.escape(", ".join(s.get("instrumentation") or []))
        cpu = s.get("cpu_percent") or {}
        mem = s.get("memory_mib") or {}
        rows.append(
            f"<tr><td>{html.escape(s.get('label', svc))}</td>"
            f"<td><code>{inst}</code></td>"
            f"<td>{html.escape(s.get('role', ''))}</td>"
            f"<td>{fmt_cpu(cpu.get('avg'))}</td><td>{fmt_cpu(cpu.get('max'))}</td>"
            f"<td>{fmt_mem(mem.get('avg'))}</td><td>{fmt_mem(mem.get('max'))}</td>"
            f"<td>{s.get('samples', '—')}</td></tr>"
        )
    if not rows:
        return '<p class="warn">無法對應 api / worker / dbwatcher 容器。</p>'
    return f"""
      <h3>Go 程式資源（instrumentation-go）</h3>
      <p class="muted-note">取樣間隔約 2s，為壓測穩態期 docker stats 平均值與峰值。</p>
      <table>
        <thead><tr>
          <th>服務</th><th>instrumentation-go</th><th>角色</th>
          <th>CPU 平均</th><th>CPU 峰值</th>
          <th>記憶體 平均</th><th>記憶體 峰值</th><th>樣本數</th>
        </tr></thead>
        <tbody>{"".join(rows)}</tbody>
      </table>"""


def render_resources_matrix(reports_dir: Path) -> str:
    """Cross-TC CPU/memory comparison for each Go service."""
    header = (
        "<tr><th>服務</th><th>指標</th>"
        + "".join(f"<th>TC{i}</th>" for i in range(1, 6))
        + "</tr>"
    )
    body_rows: list[str] = []
    for svc in ("api", "worker", "dbwatcher"):
        for metric_label, path_keys in (
            ("CPU 平均", ("cpu_percent", "avg")),
            ("CPU 峰值", ("cpu_percent", "max")),
            ("記憶體 平均", ("memory_mib", "avg")),
            ("記憶體 峰值", ("memory_mib", "max")),
        ):
            cells = []
            for tc in range(1, 6):
                res = load_resources(reports_dir / f"tc{tc}-resources.json")
                val = None
                if res:
                    s = (res.get("services") or {}).get(svc) or {}
                    block = s.get(path_keys[0]) or {}
                    val = block.get(path_keys[1])
                if path_keys[0] == "cpu_percent":
                    cells.append(f"<td>{fmt_cpu(val)}</td>")
                else:
                    cells.append(f"<td>{fmt_mem(val)}</td>")
            inst = ", ".join(GO_SERVICE_INST.get(svc, []))
            body_rows.append(
                f"<tr><td>{html.escape(svc)}<br><small>{html.escape(inst)}</small></td>"
                f"<td>{metric_label}</td>{''.join(cells)}</tr>"
            )
    return f"""
  <section class="tc matrix">
    <h2>資源對照總表（TC1–TC5）</h2>
    <p class="muted-note">僅含使用 <code>pkg/instrumentation-go</code> 的 Go 服務：api、worker、dbwatcher。</p>
    <div class="table-scroll">
      <table>
        <thead>{header}</thead>
        <tbody>{"".join(body_rows)}</tbody>
      </table>
    </div>
  </section>"""


def endpoint_rows(data: dict[str, Any]) -> list[dict[str, str]]:
    rows: list[dict[str, str]] = []
    for ep in ("jetstream", "core", "mongo"):
        key = f"http_req_duration{{endpoint:{ep}}}"
        vals = metric_values(data, key)
        rows.append(
            {
                "endpoint": ep,
                "p95_ms": fmt_ms(vals.get("p(95)")),
                "avg_ms": fmt_ms(vals.get("avg")),
                "count": fmt_int(vals.get("count")),
            }
        )
    return rows


def render_tc_section(
    tc: int,
    data: dict[str, Any] | None,
    err: str | None,
    resources: dict[str, Any] | None,
) -> str:
    meta = TC_META[tc]
    if data is None:
        return f"""
    <section class="tc failed">
      <h2>{html.escape(meta['title'])}</h2>
      <p class="err">未執行或缺少摘要：{html.escape(err or '未知錯誤')}</p>
    </section>"""

    ok, failed_th = threshold_ok(data)
    http_dur = metric_values(data, "http_req_duration")
    http_fail = metric_values(data, "http_req_failed")
    reqs = metric_values(data, "http_reqs")
    iters = metric_values(data, "iterations")
    status_class = "pass" if ok else "fail"
    status_text = "通過" if ok else "未通過"

    ep_rows = "".join(
        f"<tr><td>{html.escape(r['endpoint'])}</td>"
        f"<td>{r['p95_ms']}</td><td>{r['avg_ms']}</td><td>{r['count']}</td></tr>"
        for r in endpoint_rows(data)
    )

    th_note = ""
    if failed_th:
        th_note = '<p class="warn">未達門檻指標：' + html.escape(", ".join(failed_th)) + "</p>"

    return f"""
    <section class="tc {status_class}">
      <h2>{html.escape(meta['title'])} <span class="badge">{status_text}</span></h2>
      <dl class="meta">
        <dt>旗標</dt><dd>{html.escape(meta['flags'])}</dd>
        <dt>k6 traceparent</dt><dd>{html.escape(meta['traceparent'])}</dd>
      </dl>
      <div class="grid">
        <div class="card">
          <h3>HTTP 延遲（整體）</h3>
          <ul>
            <li>p95：<strong>{fmt_ms(http_dur.get('p(95)'))} ms</strong></li>
            <li>平均：<strong>{fmt_ms(http_dur.get('avg'))} ms</strong></li>
            <li>最大：<strong>{fmt_ms(http_dur.get('max'))} ms</strong></li>
          </ul>
        </div>
        <div class="card">
          <h3>吞吐量與錯誤</h3>
          <ul>
            <li>請求數：<strong>{fmt_int(reqs.get('count'))}</strong></li>
            <li>迭代數：<strong>{fmt_int(iters.get('count'))}</strong></li>
            <li>失敗率：<strong>{fmt_rate(http_fail.get('rate'))}</strong></li>
          </ul>
        </div>
      </div>
      {th_note}
      <h3>依端點（JetStream / Core / Mongo）</h3>
      <table>
        <thead><tr><th>端點</th><th>p95 (ms)</th><th>平均 (ms)</th><th>樣本數</th></tr></thead>
        <tbody>{ep_rows}</tbody>
      </table>
      {render_resources_table(resources)}
    </section>"""


def build_html(sections: str, matrix: str, duration: str, report_dir: Path) -> str:
    now = datetime.now(timezone.utc).astimezone()
    return f"""<!DOCTYPE html>
<html lang="zh-TW">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>otel-traces-test 壓力測試報告</title>
  <style>
    :root {{
      --bg: #0f1419;
      --card: #1a2332;
      --text: #e7ecf3;
      --muted: #8b9cb3;
      --pass: #3dd68c;
      --fail: #f07178;
      --warn: #ffcc66;
      --accent: #6cb6ff;
    }}
    * {{ box-sizing: border-box; }}
    body {{
      font-family: "PingFang TC", "Noto Sans TC", "Microsoft JhengHei", sans-serif;
      background: var(--bg);
      color: var(--text);
      line-height: 1.6;
      margin: 0;
      padding: 2rem;
      max-width: 960px;
      margin-inline: auto;
    }}
    header {{
      border-bottom: 1px solid #2d3a4d;
      padding-bottom: 1.5rem;
      margin-bottom: 2rem;
    }}
    h1 {{ font-size: 1.75rem; margin: 0 0 0.5rem; }}
    .subtitle {{ color: var(--muted); margin: 0; }}
    .tc {{
      background: var(--card);
      border-radius: 12px;
      padding: 1.25rem 1.5rem;
      margin-bottom: 1.5rem;
      border-left: 4px solid var(--muted);
    }}
    .tc.pass {{ border-left-color: var(--pass); }}
    .tc.fail {{ border-left-color: var(--fail); }}
    .tc.failed {{ border-left-color: var(--fail); opacity: 0.85; }}
    h2 {{ font-size: 1.2rem; margin: 0 0 1rem; display: flex; align-items: center; gap: 0.75rem; flex-wrap: wrap; }}
    .badge {{
      font-size: 0.75rem;
      padding: 0.15rem 0.6rem;
      border-radius: 999px;
      background: #2d3a4d;
      font-weight: 600;
    }}
    .tc.pass .badge {{ background: #1a3d2e; color: var(--pass); }}
    .tc.fail .badge {{ background: #3d1a1a; color: var(--fail); }}
    .meta {{ display: grid; grid-template-columns: auto 1fr; gap: 0.25rem 1rem; margin: 0 0 1rem; font-size: 0.9rem; }}
    .meta dt {{ color: var(--muted); margin: 0; }}
    .meta dd {{ margin: 0; }}
    .grid {{ display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }}
    @media (max-width: 640px) {{ .grid {{ grid-template-columns: 1fr; }} }}
    .card {{
      background: #121a24;
      border-radius: 8px;
      padding: 0.75rem 1rem;
    }}
    .card h3 {{ margin: 0 0 0.5rem; font-size: 0.95rem; color: var(--accent); }}
    .card ul {{ margin: 0; padding-left: 1.2rem; }}
    table {{ width: 100%; border-collapse: collapse; font-size: 0.9rem; }}
    th, td {{ text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid #2d3a4d; }}
    th {{ color: var(--muted); font-weight: 500; }}
    .warn {{ color: var(--warn); font-size: 0.9rem; }}
    .err {{ color: var(--fail); }}
    footer {{ margin-top: 2rem; color: var(--muted); font-size: 0.85rem; }}
    code {{ background: #121a24; padding: 0.1rem 0.35rem; border-radius: 4px; }}
    .muted-note {{ color: var(--muted); font-size: 0.85rem; margin: 0 0 0.75rem; }}
    .matrix {{ border-left-color: var(--accent); }}
    .table-scroll {{ overflow-x: auto; }}
    small {{ color: var(--muted); }}
  </style>
</head>
<body>
  <header>
    <h1>otel-nats / otel-mongo 全路徑壓力測試報告</h1>
    <p class="subtitle">產生時間：{html.escape(now.strftime('%Y-%m-%d %H:%M:%S %z'))} · 每案 k6 時長：{html.escape(duration)} · 路徑：JetStream、Core NATS、MongoDB（含 OTLP 匯出）</p>
  </header>
  {matrix}
  {sections}
  <footer>
    <p>原始 JSON 摘要目錄：<code>{html.escape(str(report_dir))}</code></p>
    <p>對照建議：TC1 vs TC4（instrumentation 開銷）、TC2 vs TC3 / TC4 vs TC5（取樣率對 export 的影響）。</p>
  </footer>
</body>
</html>"""


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--reports-dir", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--duration", default="1m")
    args = parser.parse_args()

    parts: list[str] = []
    any_data = False
    any_resources = False
    for tc in range(1, 6):
        path = args.reports_dir / f"tc{tc}-summary.json"
        res_path = args.reports_dir / f"tc{tc}-resources.json"
        err = None
        data = None
        resources = load_resources(res_path)
        if resources:
            any_resources = True
        if path.is_file():
            try:
                data = load_summary(path)
                any_data = True
            except (json.JSONDecodeError, OSError) as e:
                err = str(e)
        else:
            err = f"找不到 {path.name}"
        parts.append(render_tc_section(tc, data, err, resources))

    if not any_data:
        print("no summary JSON found", file=sys.stderr)
        return 1

    matrix = render_resources_matrix(args.reports_dir) if any_resources else ""
    html_doc = build_html("\n".join(parts), matrix, args.duration, args.reports_dir)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(html_doc, encoding="utf-8")
    print(f"report written: {args.output}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
