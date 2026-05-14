# TODO — Known Improvements

Items deferred from the initial implementation. Each entry describes the issue, the fix, and the trade-off that kept it out of scope.

---

## Async LLM on the Read Path

**Issue:** When statistical confidence falls below 0.70, the LLM is called synchronously inside the `GET /tests/{test_id}` handler, blocking the response for 1–2 seconds.

**Fix:** On a low-confidence read, return the statistical result immediately (optionally with a `"source": "statistical"` field), then trigger a background goroutine that calls the LLM and writes the result back to a `classifications` cache table. Subsequent reads serve the cached LLM result until it expires or new runs arrive.

**Why deferred:** Requires a new DB table, a cache invalidation strategy, and careful shutdown handling (drain the goroutine before exit). The synchronous path is acceptable at low read volume and was a deliberate simplification for the initial implementation — documented in `DESIGN.md`.

---

## Batch Ingestion

**Issue:** `POST /events` performs a synchronous single-row `INSERT` per request. At high ingest rates this creates per-event transaction overhead.

**Fix:** Validate the payload in the handler and push it onto a buffered channel. A background goroutine drains the channel in batches (e.g. up to 500 events or every 100ms, whichever comes first) and executes a single bulk `INSERT` transaction.

**Why deferred:** Adds backpressure handling, flush-on-shutdown logic, and error propagation complexity that would dominate the codebase for a prototype. SQLite with WAL handles the current load comfortably; this becomes relevant when ingest volume exceeds a few hundred events/second.

---

## Store Timestamps as Unix Epoch Integers

**Issue:** `started_at` is stored as an RFC3339 `TEXT` column. `GetHistory` calls `time.Parse` on every row returned (up to 50 per read request). SQLite string comparison for `ORDER BY started_at` is also slower than integer comparison.

**Fix:** Store `started_at` as `INTEGER` (Unix milliseconds). Parse once at ingest (`time.Time → int64`), reconstruct on read (`int64 → time.Time`). Requires a schema migration for existing databases.

**Why deferred:** Minor CPU cost at current scale; the schema change adds migration complexity. Worth revisiting before the first production deployment.
