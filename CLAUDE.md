# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a take-home interview project for Functionize (Senior Backend Engineer). The goal is to build a production-facing HTTP service in **Go or Java** that ingests continuous test execution records and classifies test health.

Full requirements are in `PROMPT.md`. Design decisions must be documented in `DESIGN.md` (one page max). A README with run/test/load-data instructions is required for submission.

## API Surface

Two endpoints only:

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

## Classification Problem

The core challenge: define what "flaky", "broken", "healthy" mean numerically, and defend it. Minimum categories are healthy / flaky / broken; additional categories (degrading, anomalous) are optional. The GET response must include confidence and reasoning alongside the classification label.

Classification strategy (heuristics, stats, LLM, or hybrid) is a design decision — document the choice and the case for/against using AI at runtime in `DESIGN.md`.

## Sample Data

`data/sample_data.jsonl` — ~5,360 JSONL records with mixed behavior across multiple test IDs. Use for development, manual validation, and seeding integration tests.

## Build & Test Commands

> Add these once the implementation language and build system are chosen. They belong in the README as well.

## Key Constraints

- `test_id` is globally unique (no multi-tenancy)
- Persistence choice is open: in-memory, SQLite, or a real DB — justify in `DESIGN.md`
- No UI, auth, deployment, or ML model training from scratch
- Tests are required on classification logic, ingestion, and key edge cases
