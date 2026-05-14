#!/usr/bin/env bash
set -euo pipefail

BIN=./test-classifier
SMOKE_ADDR=:8097
SMOKE_URL=http://localhost:8097
SMOKE_DB=/tmp/smoke-$$.db
SMOKE_LOG=smoke.log
SERVER_PID=""

cleanup() {
  if [ -n "$SERVER_PID" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -f "$SMOKE_DB"
}
trap cleanup EXIT

# ── build ─────────────────────────────────────────────────────────────────────
echo "--- building ---"
go build -o "$BIN" ./cmd/server

# ── start server ──────────────────────────────────────────────────────────────
echo "--- starting server (logs -> $SMOKE_LOG) ---"
"$BIN" -addr "$SMOKE_ADDR" -db "$SMOKE_DB" >"$SMOKE_LOG" 2>&1 &
SERVER_PID=$!

# wait until ready (up to 5 s)
for i in $(seq 1 10); do
  if curl -sf "$SMOKE_URL/tests/__probe__" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

# ── cases ─────────────────────────────────────────────────────────────────────
echo "--- unknown test → insufficient_data ---"
curl -sf "$SMOKE_URL/tests/never.seen" | jq .

echo "--- missing Content-Type → 415 ---"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$SMOKE_URL/events" \
  -d '{"test_id":"t","run_id":"r1","status":"passed","started_at":"2026-01-01T00:00:00Z"}')
echo "status=$STATUS (want 415)"

echo "--- invalid status → 400 ---"
curl -s -X POST "$SMOKE_URL/events" \
  -H 'Content-Type: application/json' \
  -d '{"test_id":"t","run_id":"r1","status":"bad","started_at":"2026-01-01T00:00:00Z"}' | jq .

echo "--- ingest 50 passing runs ---"
for i in $(seq 1 50); do
  curl -sf -X POST "$SMOKE_URL/events" \
    -H 'Content-Type: application/json' \
    -d "{\"test_id\":\"cart.happy_path\",\"run_id\":\"smoke_$i\",\"status\":\"passed\",\"duration_ms\":500,\"started_at\":\"2026-01-$(printf '%02d' $((i % 28 + 1)))T00:00:00Z\"}" \
    >/dev/null
done
echo "50 runs ingested"

echo "--- classify healthy test ---"
curl -sf "$SMOKE_URL/tests/cart.happy_path" | jq .

echo "--- ingest 10 recent failures (collapse) ---"
for i in $(seq 51 60); do
  curl -sf -X POST "$SMOKE_URL/events" \
    -H 'Content-Type: application/json' \
    -d "{\"test_id\":\"cart.happy_path\",\"run_id\":\"smoke_$i\",\"status\":\"failed\",\"duration_ms\":500,\"started_at\":\"2026-03-$(printf '%02d' $((i - 50)))T00:00:00Z\",\"error_message\":\"element not found\"}" \
    >/dev/null
done
echo "10 failure runs ingested"

echo "--- classify after collapse → broken ---"
curl -sf "$SMOKE_URL/tests/cart.happy_path" | jq .

# ── LLM escalation (only when API key is present) ────────────────────────────
if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
  echo "--- ingest 50 alternating pass/fail runs (flaky, low confidence → LLM escalation) ---"
  for i in $(seq 1 50); do
    if [ $((i % 2)) -eq 0 ]; then
      STATUS="passed"
      ERR=""
    else
      STATUS="failed"
      ERR=',"error_message":"assertion failed: expected true but got false"'
    fi
    curl -sf -X POST "$SMOKE_URL/events" \
      -H 'Content-Type: application/json' \
      -d "{\"test_id\":\"llm.smoke\",\"run_id\":\"llm_$i\",\"status\":\"$STATUS\",\"duration_ms\":300,\"started_at\":\"2026-02-$(printf '%02d' $((i % 28 + 1)))T$(printf '%02d' $((i % 24))):00:00Z\"$ERR}" \
      >/dev/null
  done
  echo "50 alternating runs ingested"

  echo "--- classify flaky test → LLM escalation ---"
  curl -sf "$SMOKE_URL/tests/llm.smoke" | jq .
else
  echo "--- skipping LLM escalation case (ANTHROPIC_API_KEY not set) ---"
fi

# ── print server logs ─────────────────────────────────────────────────────────
echo ""
echo "--- server logs ($SMOKE_LOG) ---"
cat "$SMOKE_LOG"
