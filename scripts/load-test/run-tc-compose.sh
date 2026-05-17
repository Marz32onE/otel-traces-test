#!/usr/bin/env bash
# Run load test case TC1–TC5 on docker-compose (full stack).
# Usage: ./scripts/load-test/run-tc-compose.sh <1-5>
set -euo pipefail

TC="${1:?usage: $0 <1-5>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ENV_FILE="$ROOT/load/env/tc${TC}.env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing $ENV_FILE" >&2
  exit 1
fi

# shellcheck disable=SC1090
set -a
source "$ENV_FILE"
set +a

cd "$ROOT"
COMPOSE="${COMPOSE_CMD:-docker compose}"

echo "==> TC${TC}: recreate api worker dbwatcher with env"
$COMPOSE --env-file "$ENV_FILE" up -d --build --force-recreate api worker dbwatcher

echo "==> wait for api healthy"
for i in $(seq 1 60); do
  if curl -sf -o /dev/null -X POST http://localhost:8088/api/message \
    -H 'Content-Type: application/json' -d '{"text":"ping"}' 2>/dev/null; then
    break
  fi
  sleep 2
done

NET=$($COMPOSE ps -q api | xargs docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{end}}' 2>/dev/null || true)
if [[ -z "$NET" ]]; then
  NET="otel-traces-test_default"
fi

echo "==> TC${TC}: k6 (K6_SEND_TRACEPARENT=$K6_SEND_TRACEPARENT)"
docker run --rm --network "$NET" \
  -v "$ROOT/load/k6:/scripts:ro" \
  -e API_BASE_URL=http://api:8088 \
  -e K6_SEND_TRACEPARENT \
  -e K6_DURATION="${K6_DURATION:-3m}" \
  grafana/k6:0.54.0 run /scripts/e2e-mixed.js

echo "==> TC${TC} done. pprof: curl http://localhost:6060/debug/pprof/ (expose 6060 in compose if needed)"
