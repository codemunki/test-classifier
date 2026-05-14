# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A take-home interview project for Functionize (Senior Backend Engineer). A production-facing HTTP service in Go that ingests test execution records and classifies test health using a statistical + LLM ensemble. The project is complete and submitted.

Full requirements are in `PROMPT.md`. Design decisions are in `DESIGN.md`. Deferred improvements are in `TODO.md`.

## API Surface

- `POST /events` — ingest a test execution record
- `GET /tests/{test_id}` — return current classification with confidence and reasoning

## Input Record Shape

```json
{
  "test_id": "checkout_flow_happy_path",
  "run_id": "run_9281",
  "status": "passed|failed|skipped|errored",
  "duration_ms": 4230,
  "started_at": "2026-04-12T14:02:11Z",
  "error_message": "optional"
}
```

## Build & Test Commands

All commands run from `service/`:

```bash
make build    # compile binary
make test     # run unit tests
make cover    # test with per-function coverage (internal packages only)
make run      # build and start server on :8080
make load     # stream sample_data.jsonl into a running server
make smoke    # end-to-end smoke test (starts/stops its own server)
make stress   # extended statistical + LLM escalation scenarios
make lint     # go vet
make fmt      # gofmt
```

`LOG_LEVEL` env var controls log verbosity (`debug`, `info`, `warn`, `error`). Default is `info`.

To run a single test package: `go test ./internal/classifier/...`

## Architecture

The service lives entirely in `service/`. Three internal packages:

- **`internal/classifier`** — `Classifier` interface, statistical implementation, LLM implementation (Claude Haiku 4.5 via Anthropic SDK), and ensemble that runs stat first and escalates to LLM when confidence < 0.70.
- **`internal/handler`** — HTTP handlers for both endpoints, request validation, logging middleware.
- **`internal/store`** — SQLite persistence via `modernc.org/sqlite`. Separate write (1 conn) and read (10 conn) pools; WAL mode.

Classification on `GET /tests/{test_id}` runs on every request: statistical classifier first, LLM escalation if confidence < 0.70. The statistical classifier uses a 50-run sliding window; signals are pass rate, recent trend (last 10 vs prior 40), error message diversity, and p50 duration drift.

## Sample Data

`data/sample_data.jsonl` — ~5,360 JSONL records across 100 test IDs. Load with `make load` against a running server.
