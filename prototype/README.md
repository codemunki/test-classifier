# Classifier Prototype

Rapid-prototype for exploring the sample dataset before committing to the Go implementation. Runs one or both classifiers against `data/sample_data.jsonl` and prints a comparison report.

## What it does

`classify.py` implements two classifiers behind the same interface:

- **Statistical** — windowed pass rate, trend (recent 10 vs prior 40), error message normalization, and duration drift over the last 50 runs. No external dependencies or API calls.
- **LLM** *(optional)* — sends a compact text summary of each test's run window to Claude Haiku 4.5 and asks for a JSON classification. Uses prompt caching on the shared system prompt to keep cost low.

Running both together produces a comparison report: agreement rate, divergent cases with both classifiers' reasoning, p50/p95 latency, and an estimated API cost.

### Classification categories

| Label | Definition |
|---|---|
| `healthy` | Pass rate >95%, no negative trend |
| `flaky` | Pass rate 15–95%, or inconsistent failures |
| `broken` | Pass rate <15% |
| `degrading` | Acceptable pass rate but duration increasing significantly |
| `insufficient_data` | Fewer than 5 runs |

## Setup

```bash
make setup        # create venv and install dependencies
```

This creates `.venv/` and installs `anthropic`. The venv is only needed for the `--llm` mode; the statistical classifier has no third-party dependencies.

## Running

```bash
# Statistical classifier only (no API key needed)
make run

# Both classifiers side-by-side (requires ANTHROPIC_API_KEY)
ANTHROPIC_API_KEY=sk-ant-... make run-llm

# Or explicitly:
make run-llm-only   # skip statistical output, show LLM results + comparison
```

### Manual invocation

```bash
source .venv/bin/activate

python classify.py                          # statistical only
python classify.py --llm                    # statistical + LLM comparison
python classify.py --llm-only               # LLM only (still computes stat signals internally)
```

## Output

**Statistical-only** (`make run`):

```
=== Label distribution (statistical) ===
  broken: 7
  flaky: 38
  healthy: 55

=== Low-confidence classifications (ambiguous cases) ===
  notifications.after_logout
    label=flaky confidence=0.57 pass_rate=43% trend_drop=54% error_diversity=0.44
    Pass rate 43% over 47 runs. Intermittent failures with varied errors.
  ...

=== Near-threshold cases (15-25% or 90-95% pass rate) ===
  two_factor.rate_limited: broken pass=0% conf=1.0 | Pass rate 0% over 50 runs. Dominant error pattern.
  ...

=== Duration drift > 20% ===
  notifications.after_logout: drift=+59% label=flaky pass=43%

=== Full results (sorted by pass rate) ===
  test_id                                       label               conf   pass   trend    div   drift
  ...
```

**With `--llm`**, appends:

```
=== Classifier comparison: agreement 91/100 (91%) ===

  Divergent cases (9):
  test_id                                       stat_label         llm_label          pass
  ...
    stat: Pass rate 17% over 50 runs. Consistent failure pattern...
    llm:  Low pass rate with consistent errors suggests broken rather than flaky.

  LLM latency  p50=420ms  p95=890ms  total=52.3s
  Estimated cost for 100 tests: $0.0009 (~45300 in, ~6000 out tokens, Haiku 4.5 rates)
```

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `ANTHROPIC_API_KEY` | Only for `--llm` | Anthropic API key |

## Key findings from this prototype

- 55 tests are cleanly healthy (100% pass rate, stable duration) — both classifiers agree trivially.
- 7 tests are clearly broken (<15% pass rate) — both classifiers agree with high confidence.
- The interesting region is the **38 flaky tests**, especially the 9 with confidence <0.7 where statistical signals conflict (high trend_drop against an otherwise mid-range pass rate).
- `notifications.after_logout` is the sharpest anomaly: 43% pass rate with a 59% duration drift and 54% trend drop — statistically flaky, but the LLM tends to classify it as `degrading`.
- The LLM adds defensible natural-language reasoning on ambiguous cases; the statistical classifier is faster and free at runtime.
- Likely production shape: statistical by default, LLM escalation when confidence <0.7.
