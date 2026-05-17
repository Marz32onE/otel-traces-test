#!/usr/bin/env bash
# Run TC1–TC5 load tests and emit zh-TW HTML report.
# Usage: ./scripts/load-test/run-all-report.sh
# Env: K6_DURATION (default 45s), LOAD_REPORT_DIR, COMPOSE_CMD
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPORT_DIR="${LOAD_REPORT_DIR:-$ROOT/load/reports}"
DURATION="${K6_DURATION:-45s}"
HTML_OUT="${LOAD_HTML_REPORT:-$REPORT_DIR/load-test-report.html}"
COMPOSE="${COMPOSE_CMD:-docker compose}"

mkdir -p "$REPORT_DIR"
rm -f "$REPORT_DIR"/tc*-summary.json "$REPORT_DIR"/tc*-resources.json

cd "$ROOT"
echo "==> 啟動依賴服務（mongodb、nats、otel-collector、tempo、api、worker、dbwatcher）"
$COMPOSE up -d mongodb nats otel-collector tempo api worker dbwatcher

echo "==> 等待 API 就緒"
for i in $(seq 1 90); do
  if curl -sf -o /dev/null -X POST http://localhost:8088/api/message \
    -H 'Content-Type: application/json' -d '{"text":"ready"}' 2>/dev/null; then
    echo "API ready"
    break
  fi
  if [[ "$i" -eq 90 ]]; then
    echo "API not ready after 180s" >&2
    exit 1
  fi
  sleep 2
done

NET=$($COMPOSE ps -q api | xargs docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{end}}' 2>/dev/null || true)
if [[ -z "$NET" ]]; then
  NET="otel-traces-test_default"
fi

for TC in 1 2 3 4 5; do
  ENV_FILE="$ROOT/load/env/tc${TC}.env"
  # shellcheck disable=SC1090
  set -a
  source "$ENV_FILE"
  set +a

  echo ""
  echo "========================================"
  echo " TC${TC} 開始（k6 時長 ${DURATION}）"
  echo "========================================"

  $COMPOSE --env-file "$ENV_FILE" up -d --no-build api worker dbwatcher
  sleep 5

  RESOURCES_JSON="$REPORT_DIR/tc${TC}-resources.json"
  python3 "$ROOT/scripts/load-test/collect-container-stats.py" \
    --root "$ROOT" \
    --compose-cmd "$COMPOSE" \
    --duration "$DURATION" \
    --interval "${STATS_INTERVAL:-2}" \
    --warmup "${STATS_WARMUP:-0}" \
    --output "$RESOURCES_JSON" &
  STATS_PID=$!

  SUMMARY_PATH="/reports/tc${TC}-summary.json"
  K6_EXIT=0
  if ! docker run --rm --network "$NET" \
    -v "$ROOT/load/k6:/scripts:ro" \
    -v "$REPORT_DIR:/reports" \
    -e API_BASE_URL=http://api:8088 \
    -e K6_SEND_TRACEPARENT \
    -e K6_DURATION="$DURATION" \
    -e K6_JETSTREAM_RATE="${K6_JETSTREAM_RATE:-40}" \
    -e K6_CORE_RATE="${K6_CORE_RATE:-30}" \
    -e K6_MONGO_RATE="${K6_MONGO_RATE:-30}" \
    -e "K6_SUMMARY_PATH=$SUMMARY_PATH" \
    grafana/k6:0.54.0 run /scripts/e2e-mixed.js; then
    K6_EXIT=1
    echo "WARN: TC${TC} k6 exited non-zero (thresholds may have failed); continuing" >&2
  fi

  kill "$STATS_PID" 2>/dev/null || true
  wait "$STATS_PID" 2>/dev/null || true
done

echo ""
echo "==> 產生 HTML 報告"
python3 "$ROOT/scripts/load-test/generate-html-report.py" \
  --reports-dir "$REPORT_DIR" \
  --output "$HTML_OUT" \
  --duration "$DURATION"

echo ""
echo "完成：$HTML_OUT"
if command -v open >/dev/null 2>&1; then
  open "$HTML_OUT" || true
fi
