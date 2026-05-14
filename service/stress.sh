#!/usr/bin/env bash
# stress.sh — extended classification scenarios for statistical and LLM escalation paths.
#
# Statistical scenarios run unconditionally and assert exact labels.
# LLM escalation scenarios require ANTHROPIC_API_KEY and print full responses.
#
# Set LOG_LEVEL=debug to see raw LLM responses and escalation decisions in stress.log.
set -euo pipefail

BIN=./test-classifier
STRESS_ADDR=:8098
STRESS_URL=http://localhost:8098
STRESS_DB=/tmp/stress-$$.db
STRESS_LOG=stress.log
SERVER_PID=""
PASS=0
FAIL=0

cleanup() {
  if [ -n "$SERVER_PID" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -f "$STRESS_DB"
}
trap cleanup EXIT

# ── helpers ────────────────────────────────────────────────────────────────────

# date_for <i> — monotonically increasing RFC3339 timestamp for run index i (1-based).
# i=1..28 → 2026-01-01..28, i=29..50 → 2026-02-01..22. No wrapping.
date_for() {
  local i=$1
  local month=$(( (i + 27) / 28 ))
  local day=$(( (i - 1) % 28 + 1 ))
  printf "2026-%02d-%02dT00:00:00Z" "$month" "$day"
}

assert_label() {
  local test_id="$1" expected="$2"
  local actual confidence response
  response=$(curl -sf "$STRESS_URL/tests/$test_id")
  actual=$(echo "$response" | jq -r .label)
  confidence=$(echo "$response" | jq -r .confidence)
  if [ "$actual" = "$expected" ]; then
    printf "  PASS  %-30s label=%-20s confidence=%s\n" "$test_id" "$actual" "$confidence"
    PASS=$((PASS + 1))
  else
    printf "  FAIL  %-30s label=%-20s confidence=%s  (expected %s)\n" "$test_id" "$actual" "$confidence" "$expected"
    FAIL=$((FAIL + 1))
  fi
}

assert_label_one_of() {
  local test_id="$1"; shift
  local expected_list=("$@")
  local actual confidence response
  response=$(curl -sf "$STRESS_URL/tests/$test_id")
  actual=$(echo "$response" | jq -r .label)
  confidence=$(echo "$response" | jq -r .confidence)
  for expected in "${expected_list[@]}"; do
    if [ "$actual" = "$expected" ]; then
      printf "  PASS  %-30s label=%-20s confidence=%s\n" "$test_id" "$actual" "$confidence"
      PASS=$((PASS + 1))
      return
    fi
  done
  printf "  FAIL  %-30s label=%-20s confidence=%s  (expected one of: %s)\n" \
    "$test_id" "$actual" "$confidence" "${expected_list[*]}"
  FAIL=$((FAIL + 1))
}

ingest() {
  local test_id="$1" run_id="$2" status="$3" duration="$4" started_at="$5" error_msg="${6:-}"
  local body="{\"test_id\":\"$test_id\",\"run_id\":\"$run_id\",\"status\":\"$status\",\"duration_ms\":$duration,\"started_at\":\"$started_at\""
  [ -n "$error_msg" ] && body="$body,\"error_message\":\"$error_msg\""
  body="$body}"
  curl -sf -X POST "$STRESS_URL/events" -H 'Content-Type: application/json' -d "$body" >/dev/null
}

show_result() {
  local test_id="$1"
  curl -sf "$STRESS_URL/tests/$test_id" | jq .
}

# ── build ──────────────────────────────────────────────────────────────────────
echo "--- building ---"
go build -o "$BIN" ./cmd/server

# ── start server ───────────────────────────────────────────────────────────────
echo "--- starting server (logs -> $STRESS_LOG, level=${LOG_LEVEL:-info}) ---"
"$BIN" -addr "$STRESS_ADDR" -db "$STRESS_DB" >"$STRESS_LOG" 2>&1 &
SERVER_PID=$!

for i in $(seq 1 10); do
  if curl -sf "$STRESS_URL/tests/__probe__" >/dev/null 2>&1; then break; fi
  sleep 0.5
done

# ══════════════════════════════════════════════════════════════════════════════
# STATISTICAL PATH  (stat confidence ≥ 0.70 — no LLM escalation)
# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Statistical path (no LLM escalation) ==="

# ── 1. Clearly broken: 2/40 passing (5%) → conf 1.0 ──────────────────────────
echo ""
echo "--- clearly broken (5% pass rate) ---"
for i in $(seq 1 50); do
  if [ "$i" -eq 25 ] || [ "$i" -eq 50 ]; then
    ingest "stat.clearly_broken" "cb_$i" "passed" 400 "$(date_for "$i")"
  else
    ingest "stat.clearly_broken" "cb_$i" "failed" 400 "$(date_for "$i")" "assertion error"
  fi
done
assert_label "stat.clearly_broken" "broken"

# ── 2. Near-healthy: 48/50 passing (96%) → conf 0.76 ─────────────────────────
# Failures at i=15 and i=30 — both in the historical window (runs 1-40).
# Recent window (41-50) is all-passing, so drop = historicalPR(95%) - recentPR(100%) < 0.
echo ""
echo "--- near-healthy (96% pass rate, failures in historical window) ---"
for i in $(seq 1 50); do
  if [ "$i" -eq 15 ] || [ "$i" -eq 30 ]; then
    ingest "stat.near_healthy" "nh_$i" "failed" 300 "$(date_for "$i")" "timeout"
  else
    ingest "stat.near_healthy" "nh_$i" "passed" 300 "$(date_for "$i")"
  fi
done
assert_label "stat.near_healthy" "healthy"

# ── 3. Consistent error: 10/50 passing (20%), all failures same message ───────
# Low error diversity (diversity→0) gives flaky/0.80 — stat is confident without LLM.
echo ""
echo "--- consistent error (20% pass, identical error message) ---"
for i in $(seq 1 40); do
  ingest "stat.consistent_error" "ce_$i" "failed" 500 "$(date_for "$i")" "element submit-btn not found"
done
for i in $(seq 41 50); do
  ingest "stat.consistent_error" "ce_$i" "passed" 500 "$(date_for "$i")"
done
assert_label_one_of "stat.consistent_error" "flaky" "broken"

# ══════════════════════════════════════════════════════════════════════════════
# LLM ESCALATION PATH  (stat confidence < 0.70 — ensemble triggers LLM)
# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== LLM escalation path ==="

if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
  echo "  (ANTHROPIC_API_KEY not set — running stat classifier only, skipping LLM assertions)"
fi

# ── 4. Recovering: 30 old failures, 20 recent passes ─────────────────────────
# Overall 40% pass rate → flaky/0.60. Recent 10 (i=41-50) all passing.
# Stat says flaky; LLM recognises the recovery trend.
echo ""
echo "--- recovering (30 failures then 20 passes, 40% overall) ---"
for i in $(seq 1 30); do
  ingest "llm.recovering" "rec_$i" "failed" 300 "$(date_for "$i")" "assertion failed"
done
for i in $(seq 31 50); do
  ingest "llm.recovering" "rec_$i" "passed" 300 "$(date_for "$i")"
done
assert_label_one_of "llm.recovering" "flaky" "healthy"
echo "  full response:"
show_result "llm.recovering"

# ── 5. Duration degrading: 84% pass, duration triples (208ms → 600ms) ────────
# Uses monotonically increasing dates so sort order preserves duration ramp.
# driftRatio ≈ 66% > 30% and passRate 84% > 70% → degrading/0.65 → escalates.
echo ""
echo "--- duration degrading (84% pass, 200ms → 600ms) ---"
for i in $(seq 1 50); do
  DURATION=$(( 200 + i * 8 ))   # 208ms → 600ms
  if [ $((i % 6)) -eq 0 ]; then
    ingest "llm.degrading" "deg_$i" "failed" "$DURATION" "$(date_for "$i")" "request timed out"
  else
    ingest "llm.degrading" "deg_$i" "passed" "$DURATION" "$(date_for "$i")"
  fi
done
assert_label_one_of "llm.degrading" "degrading" "flaky"
echo "  full response:"
show_result "llm.degrading"

# ── 6. Borderline flaky: 40% pass, varied error messages ─────────────────────
# passRate 40% → flaky/0.60. High error diversity, no dominant pattern.
echo ""
echo "--- borderline flaky (40% pass, varied errors) ---"
for i in $(seq 1 50); do
  if [ $((i % 10)) -lt 4 ]; then
    ingest "llm.borderline_flaky" "bf_$i" "passed" 400 "$(date_for "$i")"
  else
    ingest "llm.borderline_flaky" "bf_$i" "failed" 400 "$(date_for "$i")" \
      "error variant $((i % 7)): unexpected response"
  fi
done
assert_label_one_of "llm.borderline_flaky" "flaky" "broken"
echo "  full response:"
show_result "llm.borderline_flaky"

# ── summary ────────────────────────────────────────────────────────────────────
echo ""
echo "--- server logs ($STRESS_LOG) ---"
cat "$STRESS_LOG"

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] || exit 1
