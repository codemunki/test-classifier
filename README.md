# test-classifier

An HTTP service that ingests a continuous stream of test execution records and classifies the health of each test as `healthy`, `flaky`, `broken`, `degrading`, or `insufficient_data`. Classification is computed on every read from a sliding window of the last 50 runs — no offline batch job required.

See `DESIGN.md` for the full rationale behind the classification strategy, persistence choice, and empirical comparison between the statistical and LLM classifiers.

## Requirements

- Go 1.22+
- No other runtime dependencies — the SQLite driver is pure Go (no cgo, no system libraries)

## Quick start

```bash
cd service
make run        # builds and starts on :8080, creates classifier.db
make load       # (separate terminal) streams sample data into the running server
```

Then query a test:

```bash
curl http://localhost:8080/tests/notifications.after_logout
```

## Dependencies

| Dependency | Purpose |
|---|---|
| `modernc.org/sqlite` | Pure-Go SQLite driver — no cgo required |
| `github.com/anthropics/anthropic-sdk-go` | Claude API client for the LLM classifier |

Both are declared in `service/go.mod` and fetched automatically by `go build`.

## Configuration

The server accepts two flags:

| Flag | Default | Description |
|---|---|---|
| `-addr` | `:8080` | TCP listen address |
| `-db` | `classifier.db` | SQLite database file path |

```bash
./test-classifier -addr :9090 -db /var/data/classifier.db
```

### LLM escalation

If `ANTHROPIC_API_KEY` is set at startup, the service activates an ensemble classifier: statistical by default, Claude Haiku 4.5 for tests where statistical confidence falls below 0.70 (~9% of tests in the sample data). Without the key, the statistical classifier runs alone with no degradation in core functionality.

```bash
ANTHROPIC_API_KEY=sk-ant-... make run
```

## API

### `POST /events` — ingest a run

```bash
curl -X POST http://localhost:8080/events \
  -H 'Content-Type: application/json' \
  -d '{
    "test_id":       "checkout.happy_path",
    "run_id":        "run_9281",
    "status":        "failed",
    "duration_ms":   4230,
    "started_at":    "2026-04-12T14:02:11Z",
    "error_message": "Expected element #submit-btn to be visible after 5000ms"
  }'
```

**Request fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `test_id` | string | yes | Unique identifier for the test |
| `run_id` | string | yes | Unique identifier for this execution |
| `status` | string | yes | One of `passed`, `failed`, `skipped`, `errored` |
| `duration_ms` | integer | no | Execution time in milliseconds |
| `started_at` | string | yes | RFC3339 timestamp |
| `error_message` | string | no | Failure detail, used for error pattern analysis |

**Responses:**

| Status | Meaning |
|---|---|
| `202 Accepted` | Record stored |
| `400 Bad Request` | Validation failure — response body contains `{"error": "..."}` |

Duplicate `run_id`s are silently ignored — the endpoint is idempotent.

---

### `GET /tests/{test_id}` — get classification

```bash
curl http://localhost:8080/tests/checkout.happy_path
```

```json
{
  "test_id":    "checkout.happy_path",
  "label":      "flaky",
  "confidence": 0.75,
  "reasoning":  "Pass rate 43% over 47 runs. Intermittent failures with varied errors."
}
```

**Response fields:**

| Field | Type | Description |
|---|---|---|
| `test_id` | string | The requested test ID |
| `label` | string | Classification — see table below |
| `confidence` | float | Classifier certainty, 0.0–1.0 |
| `reasoning` | string | Human-readable explanation of the key signal |

**Classification labels:**

| Label | Meaning |
|---|---|
| `healthy` | Pass rate >95% over the last 50 runs, no negative trend |
| `flaky` | Pass rate 15–95%, or inconsistent failures |
| `broken` | Pass rate <15%, or recent-window collapse to near 0% |
| `degrading` | Acceptable pass rate but p50 duration increasing significantly |
| `insufficient_data` | Fewer than 5 non-skipped runs in the window |

Always returns `200 OK`. Unknown tests return `insufficient_data`.

## How classification works

On each `GET /tests/{test_id}` call the service:

1. Fetches the most recent 50 runs for the test from SQLite
2. Computes signals over that window:
   - **Pass rate** — overall and split into recent (last 10) vs historical (prior 40)
   - **Trend** — drop between historical and recent pass rates
   - **Recent collapse** — if recent-window pass rate is near 0% despite a higher overall rate, the test is classified `broken` regardless of the overall window (catches tests that were healthy then suddenly stopped passing)
   - **Error diversity** — normalized error messages (numbers and URLs stripped) are deduplicated to approximate semantic clustering; low diversity signals a dominant repeating failure
   - **Duration drift** — p50 duration change between first and second half of the window; >30% increase on a passing test triggers `degrading`
3. Returns the label, confidence (derived from how cleanly the signals map to a category), and a one-sentence reasoning string

If `ANTHROPIC_API_KEY` is set and statistical confidence is below 0.70, a compact summary of the run window is sent to Claude Haiku 4.5, which returns its own label, confidence, and reasoning. The LLM response is returned directly. If the API call fails, the statistical result is used as fallback.

## Run the tests

```bash
cd service
make test
```

All tests use an in-memory SQLite database — no setup or teardown needed.

**Test coverage:**

| Package | What's tested |
|---|---|
| `internal/classifier` | 7 BDD scenarios: healthy, flaky, broken, insufficient\_data (two cases), recent collapse, duration drift |
| `internal/store` | Insert and retrieve, duplicate run\_id idempotency, unknown test returns empty, limit enforcement, distinct test IDs |
| `internal/handler` | Valid ingest, missing fields, invalid status, bad timestamp, malformed JSON, duplicate idempotency, classify after ingestion, unknown test |

To run a single package:

```bash
go test ./internal/classifier/
go test ./internal/store/
go test ./internal/handler/
```

## Smoke test

```bash
cd service
make smoke
```

Builds the binary, starts the server on a temporary port and database, exercises the API end-to-end, then shuts down cleanly. No setup required — the server is started and stopped automatically.

**What it covers:**

| Case | Expected |
|---|---|
| `GET /tests/never.seen` | `insufficient_data` — no runs yet |
| `POST /events` without `Content-Type: application/json` | `415 Unsupported Media Type` |
| `POST /events` with invalid `status` | `400 Bad Request` with error message |
| Ingest 50 passing runs, then `GET` | `healthy` — 100% pass rate |
| Ingest 10 recent failures on top, then `GET` | `broken` — recent-collapse detection fires |
| Ingest 50 alternating pass/fail runs, then `GET` (requires `ANTHROPIC_API_KEY`) | LLM escalation fires — statistical confidence < 0.70 |

Server logs are written to `smoke.log` as a record of the run. The log captures every request (method, path, status, latency) and every classification (test\_id, label, confidence, duration):

```
request method=POST path=/events status=202 duration_ms=1
classify test_id=cart.happy_path label=broken confidence=0.85 duration_ms=0
request method=GET path=/tests/cart.happy_path status=200 duration_ms=1
```

When `ANTHROPIC_API_KEY` is set, LLM escalation log lines are also captured:

```
llm test_id=checkout.third_party_failure duration_ms=1342 input_tokens=312 output_tokens=48 cost_usd=0.000552
```

## Load the sample data

With the server running on `:8080`:

```bash
cd service
make load
# done: 5360 sent, 0 skipped
```

The loader (`cmd/load`) reads `data/sample_data.jsonl` line by line and POSTs each record to `/events`. To target a different address:

```bash
go run ./cmd/load -addr http://localhost:9090 -file ../data/sample_data.jsonl
```

Interesting tests to query after loading:

```bash
curl http://localhost:8080/tests/two_factor.rate_limited    # broken — 0% pass rate
curl http://localhost:8080/tests/notifications.after_logout # broken — recent collapse, 59% duration drift
curl http://localhost:8080/tests/cart.as_guest              # healthy — 100% pass rate
curl http://localhost:8080/tests/login.rate_limited         # flaky — consistent failure pattern
```

## Project structure

```
service/
  cmd/server/      entry point, flag parsing, dependency wiring
  cmd/load/        sample data loader
  internal/
    classifier/    Classifier interface, Run/Result types, statistical,
                   LLM, and ensemble implementations
    store/         SQLite schema, InsertRun, GetHistory
    handler/       POST /events and GET /tests/{test_id} handlers
  smoke.sh         end-to-end smoke test script
  go.mod / go.sum
data/
  sample_data.jsonl   ~5,360 records across 100 test IDs
prototype/         Python prototype: statistical + LLM comparison against sample data
  classify.py
  results-llm.txt  Full comparison output (87% agreement, 13 divergent cases)
DESIGN.md          Design decisions and classifier comparison findings
```
