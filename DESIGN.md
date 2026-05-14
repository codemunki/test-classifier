# Design

## Language

Go. Minimal boilerplate for an HTTP service, fast compile/run cycle, and the standard library covers everything needed. Easier to produce a clean checkout experience than Java.

## Persistence

SQLite. In-memory dies on restart, which undercuts production-facing intent. PostgreSQL requires infrastructure. SQLite is a defensible real choice — one file, zero setup, survives restarts, and handles the expected write volume without issue.

## Classification Categories

| Label | Definition |
|---|---|
| `healthy` | Pass rate >95% over recent window, no negative trend |
| `flaky` | Pass rate 15–95%, or inconsistent failure pattern |
| `broken` | Pass rate <15%, or recent window near 0% |
| `degrading` | Pass rate acceptable but p95 duration increasing significantly |
| `insufficient_data` | Fewer than 5 runs — no classification made |

`anomalous` was considered (sudden step-change in duration, bimodal pass/fail patterns, abrupt error message shifts) but not implemented as a distinct label. These patterns are structurally ambiguous — defining them precisely enough to classify with high confidence via heuristics is difficult, and adding a vague catch-all label reduces rather than increases signal for the user. In practice, tests with unusual signal combinations fall below the statistical confidence threshold (0.70) and are escalated to the LLM, which reasons about the pattern in natural language. The LLM path handles the `anomalous` space more defensibly than a hard-coded category would.

## Classification Strategy

The prompt explicitly asks whether to use AI at runtime and expects a defence either way. Rather than argue theoretically, we implement both behind a common `Classifier` interface and let the data decide.

**Statistical classifier** — computes signals over a sliding window of the last 50 runs:
- Pass rate and recent trend (last 10 vs prior 40)
- Error message consistency after normalization (strip numbers, lowercase) to approximate clustering without embeddings
- Duration drift (p95 trend)

Confidence is derived from sample size and how cleanly the test maps to a category. Reasoning is assembled from the signal values as a human-readable string.

**LLM classifier** — summarises the run history and sends it to Claude with a structured prompt requesting a classification label, confidence (0–1), and reasoning.

## Development Approach: TDD + BDD

The `Classifier` interface is defined first. BDD scenarios — derived from recognisable patterns in the sample data — are written before either implementation:

- *Given* a test with 48/50 recent runs passing and stable duration → *expect* `healthy`
- *Given* a test with ~50% pass rate and varied error messages → *expect* `flaky`
- *Given* a test with <15% pass rate over last 50 runs → *expect* `broken`
- *Given* a test with fewer than 5 runs → *expect* `insufficient_data`

Both classifiers must satisfy the same BDD suite. These scenarios serve as ground-truth anchors, solving the absence of pre-labelled data: clear cases establish correctness; cases where classifiers diverge are the analytically interesting region.

## Validation

Both classifiers were run against all 100 tests in the sample dataset using a Python prototype (`prototype/classify.py`). Full output is in `prototype/results-llm.txt`.

**Agreement: 87/100 (87%).** The 13 divergent cases split into two patterns:

1. **Recent collapse hidden by historical pass rate** (6 tests, 17–43% overall pass rate): all had 0% pass rate in the last 10 runs. The statistical classifier labels these `flaky` because the all-window pass rate is above 15%; the LLM correctly identifies the recent collapse and calls them `broken`. This is a genuine gap in the statistical classifier — fixed in the implementation by adding a secondary check: if the recent-window pass rate is near zero, override to `broken` regardless of the overall window.

2. **Duration drift on 100% pass rate tests** (5 tests): the statistical classifier calls these `healthy`; the LLM calls them `degrading` based on 9–18% p50 duration increases. These are defensible either way — the duration increases are mild. The statistical `degrading` threshold (>30% drift) was left as-is; these cases sit below it.

**Latency:** statistical ~0ms, LLM p50=1.3s / p95=1.8s per test. Not viable for the hot path.

**Cost:** ~$0.05 per full 100-test run on Haiku 4.5 with prompt caching. Acceptable for a background re-classification job; too expensive to run on every ingested event.

**Production conclusion:** statistical classifier on every ingestion event; LLM re-classification as a background job for tests where statistical confidence falls below 0.7 (9 tests in this dataset — the ones with conflicting signals). The LLM's value is its natural-language reasoning on ambiguous cases, not raw classification accuracy on clear ones.

## What Was Cut

- Auth, multi-tenancy, UI — explicitly out of scope
- Embedding-based error clustering — approximated well enough by string normalization for this scale
- Exhaustive test coverage — tests focus on classification logic, ingestion, and edge cases (insufficient data, all-skipped, conflicting signals)

## AI Usage

**Build-time:** Claude Code used throughout — scaffolding, boilerplate, test generation.

**Runtime:** The LLM classifier calls the Claude API directly. The statistical classifier uses no AI at runtime.
