# Functionize — Senior Backend Engineer Take-Home

Thanks for taking the time to do this. Please read the whole prompt before you start.

---

## The rules

- **Time:** aim for roughly 3–4 hours of focused work. We'd rather see what you'd ship in that window than something polished that took your whole weekend.
- **Window:** 5 calendar days from receipt.
- **Language:** Go or Java — your choice, explained in `DESIGN.md`.
- **AI tools** (Copilot, Cursor, Claude, etc.) are allowed and encouraged. Be ready to discuss what you used and where you overrode it.
- **Tests and a `DESIGN.md` are required.** A submission without either is treated as incomplete.

---

## The problem

Functionize runs millions of automated tests on behalf of our customers, against *their* applications. A continuous stream of test execution results flows back into our platform.

You're building the service that consumes that stream and **classifies what's happening** with each test — is it healthy, flaky, broken, or something else worth surfacing.

The hard part isn't the ingestion. The hard part is deciding what "flaky" actually means, and being able to defend it.

**Treat this as a production-facing service.** What you ship should be code you'd be comfortable putting on call for: real error handling, sensible failure paths, structure you'd want to maintain. Production-facing doesn't mean production-complete in 3 hours — it means your choices and the bar for the code you write should reflect that this would ship to customers.

---

## Input

Records arrive as a continuous stream. A record looks roughly like:

```json
{
  "test_id": "checkout_flow_happy_path",
  "run_id": "run_9281",
  "status": "failed",
  "duration_ms": 4230,
  "started_at": "2026-04-12T14:02:11Z",
  "error_message": "Expected element '#submit-btn' to be visible after 5000ms"
}
```

Status is one of `passed`, `failed`, `skipped`, `errored`.

We'll send a sample dataset (~5,000 records, mixed behavior) alongside this prompt. Use it for development and testing.

---

## What to build

An HTTP service that supports at least:

1. **`POST /events`** — ingest a test execution record.
2. **`GET /tests/{test_id}`** — return the current classification of that test, with confidence and reasoning.

That's it for the API surface.

---

## What's intentionally underspecified

Make these calls and defend them in your `DESIGN.md`:

- **Your categories** and how you define them. Healthy / flaky / broken is the obvious start. Going further (e.g., degrading, anomalous) is up to you.
- **Your classification strategy** — heuristics, statistics, ML, an LLM, or a hybrid. We're especially interested in whether you reach for AI, and if so why — including the case for *not* using it here.
- **Persistence** — any DB is fine, including in-memory or SQLite. Your choice.

---

## What to submit

A private Git repo containing:

1. **Code** — runnable from a clean checkout.
2. **Tests** — on what matters (classification, ingestion, key edge cases).
3. **README** — how to run it, how to run the tests, how to load the sample data.
4. **`DESIGN.md`** (one page max) covering: language choice, persistence choice, your categories and classification strategy, what you cut and why, and how you used AI both while building and (if at all) in the runtime.

---

## Out of scope

Skip these — they're not what we're evaluating:

- UI, deployment, authentication.
- Multi-tenancy / customer isolation. Assume `test_id` is globally unique.
- Exhaustive test coverage.
- Anything sophisticated for its own sake.
- Training a new ML model from scratch.

---

## Questions?

If something is ambiguous and you can't resolve it with a reasonable assumption, email your recruiter. Good luck.