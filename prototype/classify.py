#!/usr/bin/env python3
"""
Rapid prototype: statistical + LLM classifier against sample_data.jsonl.
Goal: understand whether the data is intentionally designed to resist
naive classification, and where the two classifier approaches are likely
to diverge.

Usage:
  python classify.py             # statistical only
  python classify.py --llm       # both classifiers (requires ANTHROPIC_API_KEY)
  python classify.py --llm-only  # LLM only
"""

import json
import os
import re
import sys
import time
from collections import Counter, defaultdict
from dataclasses import dataclass
from pathlib import Path
from statistics import median

WINDOW = 50  # runs to consider
RECENT = 10  # "recent" slice within the window
HEALTHY_THRESHOLD = 0.95
BROKEN_THRESHOLD = 0.15
MIN_RUNS = 5
TREND_DROP = 0.15  # recent pass rate drop vs historical that signals degrading


@dataclass
class Classification:
    label: str
    confidence: float
    reasoning: str
    signals: dict


def normalize_error(msg: str) -> str:
    """Strip numbers and URLs to group semantically similar errors."""
    msg = msg.lower()
    msg = re.sub(r"https?://\S+", "<url>", msg)
    msg = re.sub(r"\d+ms", "<ms>", msg)
    msg = re.sub(r"\d+", "<n>", msg)
    msg = msg.strip()
    return msg


def classify(test_id: str, history: list[dict]) -> Classification:
    history = sorted(history, key=lambda r: r["started_at"])
    window = history[-WINDOW:]

    if len(window) < MIN_RUNS:
        return Classification(
            "insufficient_data",
            1.0,
            f"Only {len(window)} runs available.",
            {"run_count": len(window)},
        )

    non_skipped = [r for r in window if r["status"] != "skipped"]
    if not non_skipped:
        return Classification(
            "insufficient_data",
            1.0,
            "All runs in window were skipped.",
            {"run_count": len(window)},
        )

    passed = [r for r in non_skipped if r["status"] == "passed"]
    pass_rate = len(passed) / len(non_skipped)

    # Trend: compare recent slice vs rest of window
    recent = [r for r in non_skipped[-RECENT:]]
    historical = [r for r in non_skipped[:-RECENT]] if len(non_skipped) > RECENT else []
    recent_pass_rate = (
        sum(1 for r in recent if r["status"] == "passed") / len(recent)
        if recent
        else pass_rate
    )
    historical_pass_rate = (
        sum(1 for r in historical if r["status"] == "passed") / len(historical)
        if historical
        else pass_rate
    )
    trend_drop = historical_pass_rate - recent_pass_rate

    # Error consistency
    failures = [r for r in non_skipped if r["status"] in ("failed", "errored")]
    error_msgs = [
        normalize_error(r.get("error_message", ""))
        for r in failures
        if r.get("error_message")
    ]
    unique_errors = len(set(error_msgs))
    error_diversity = unique_errors / len(error_msgs) if error_msgs else 0.0

    # Duration drift
    durations = [r["duration_ms"] for r in window if "duration_ms" in r]
    half = len(durations) // 2
    early_durations = durations[:half]
    late_durations = durations[half:]
    duration_drift = 0.0
    if early_durations and late_durations:
        early_med = median(early_durations)
        late_med = median(late_durations)
        duration_drift = (late_med - early_med) / early_med if early_med > 0 else 0.0

    signals = {
        "run_count": len(window),
        "pass_rate": round(pass_rate, 3),
        "recent_pass_rate": round(recent_pass_rate, 3),
        "historical_pass_rate": round(historical_pass_rate, 3),
        "trend_drop": round(trend_drop, 3),
        "failure_count": len(failures),
        "unique_errors": unique_errors,
        "error_diversity": round(error_diversity, 3),
        "duration_drift": round(duration_drift, 3),
    }

    # Classification logic
    if pass_rate > HEALTHY_THRESHOLD and trend_drop < TREND_DROP:
        confidence = min(1.0, 0.7 + (pass_rate - HEALTHY_THRESHOLD) * 6)
        return Classification(
            "healthy",
            round(confidence, 2),
            f"Pass rate {pass_rate:.0%} over {len(window)} runs. No negative trend.",
            signals,
        )

    if pass_rate < BROKEN_THRESHOLD:
        confidence = min(1.0, 0.7 + (BROKEN_THRESHOLD - pass_rate) * 6)
        dominant = (
            " Dominant error pattern."
            if error_diversity < 0.4
            else " Errors are varied."
        )
        return Classification(
            "broken",
            round(confidence, 2),
            f"Pass rate {pass_rate:.0%} over {len(window)} runs.{dominant}",
            signals,
        )

    if pass_rate > HEALTHY_THRESHOLD and trend_drop >= TREND_DROP:
        return Classification(
            "degrading",
            0.7,
            f"Pass rate still {pass_rate:.0%} but dropped {trend_drop:.0%} recently. "
            f"Duration drift {duration_drift:+.0%}.",
            signals,
        )

    if duration_drift > 0.3 and pass_rate > 0.7:
        return Classification(
            "degrading",
            0.65,
            f"Pass rate {pass_rate:.0%} but p50 duration up {duration_drift:.0%} in recent runs.",
            signals,
        )

    # Flaky — but characterise it
    confidence = 0.5 + abs(0.5 - pass_rate)  # higher confidence toward the extremes
    if error_diversity < 0.4:
        flavour = (
            "consistent failure pattern — may indicate a recurring environment issue"
        )
    elif pass_rate < 0.35:
        flavour = "severe failure rate with varied errors — borderline broken"
    else:
        flavour = "intermittent failures with varied errors"
    return Classification(
        "flaky",
        round(confidence, 2),
        f"Pass rate {pass_rate:.0%} over {len(window)} runs. {flavour.capitalize()}.",
        signals,
    )


VALID_LABELS = {"healthy", "flaky", "broken", "degrading", "insufficient_data"}

LLM_SYSTEM_PROMPT = """You classify automated test health from run history.

Categories:
- healthy: pass rate >95% in recent window, no negative trend
- flaky: pass rate 15-95%, or inconsistent failures
- broken: pass rate <15%
- degrading: acceptable pass rate but duration increasing significantly
- insufficient_data: fewer than 5 runs

Respond with valid JSON only:
{"label": "<category>", "confidence": <0.0-1.0>, "reasoning": "<one sentence>"}

Rules:
- label must be exactly one of the five categories above
- confidence is your certainty about the label (not the test's reliability)
- reasoning is one clear sentence citing the key signal"""


def _summarise_for_llm(test_id: str, history: list[dict]) -> str:
    """Compact text summary of recent run history for the LLM prompt."""
    history = sorted(history, key=lambda r: r["started_at"])
    window = history[-WINDOW:]
    non_skipped = [r for r in window if r["status"] != "skipped"]

    lines = [f"test_id: {test_id}", f"runs_in_window: {len(window)}"]

    if non_skipped:
        passed = sum(1 for r in non_skipped if r["status"] == "passed")
        lines.append(
            f"pass_rate: {passed / len(non_skipped):.2f} ({passed}/{len(non_skipped)})"
        )

        recent = non_skipped[-RECENT:]
        old = non_skipped[:-RECENT] if len(non_skipped) > RECENT else []
        if old:
            recent_pr = sum(1 for r in recent if r["status"] == "passed") / len(recent)
            old_pr = sum(1 for r in old if r["status"] == "passed") / len(old)
            lines.append(f"recent_{RECENT}_pass_rate: {recent_pr:.2f}")
            lines.append(f"older_pass_rate: {old_pr:.2f}")

        durations = [r["duration_ms"] for r in window if "duration_ms" in r]
        if len(durations) >= 4:
            half = len(durations) // 2
            early_med = median(durations[:half])
            late_med = median(durations[half:])
            drift = (late_med - early_med) / early_med if early_med > 0 else 0.0
            lines.append(
                f"duration_drift: {drift:+.0%} "
                f"(early_p50={early_med:.0f}ms, late_p50={late_med:.0f}ms)"
            )

        failures = [r for r in non_skipped if r["status"] in ("failed", "errored")]
        if failures:
            errors = [
                normalize_error(r.get("error_message", ""))
                for r in failures
                if r.get("error_message")
            ]
            unique = list(dict.fromkeys(errors))[:3]  # top 3 distinct normalized errors
            lines.append(f"failure_count: {len(failures)}")
            lines.append(f"sample_errors: {unique}")
    else:
        lines.append("all_runs_skipped: true")

    return "\n".join(lines)


def classify_llm(test_id: str, history: list[dict], client) -> Classification:
    """LLM-based classifier using Claude with prompt caching on the system prompt."""
    summary = _summarise_for_llm(test_id, history)

    try:
        resp = client.messages.create(
            model="claude-haiku-4-5",
            max_tokens=256,
            system=[
                {
                    "type": "text",
                    "text": LLM_SYSTEM_PROMPT,
                    "cache_control": {"type": "ephemeral"},
                }
            ],
            messages=[{"role": "user", "content": summary}],
        )
        raw = resp.content[0].text.strip()
        # Strip markdown code fences if present
        if raw.startswith("```"):
            raw = re.sub(r"^```[a-z]*\n?", "", raw)
            raw = re.sub(r"\n?```$", "", raw)
        data = json.loads(raw)
        label = data.get("label", "").lower().strip()
        if label not in VALID_LABELS:
            label = "flaky"
        confidence = float(data.get("confidence", 0.5))
        reasoning = data.get("reasoning", "")
        return Classification(label, round(confidence, 2), reasoning, {"llm": True})
    except Exception as e:
        return Classification(
            "flaky", 0.5, f"LLM error: {e}", {"llm": True, "error": str(e)}
        )


def main():
    run_llm = "--llm" in sys.argv or "--llm-only" in sys.argv
    stat_only = "--llm-only" not in sys.argv

    client = None
    if run_llm:
        try:
            import anthropic

            api_key = os.environ.get("ANTHROPIC_API_KEY")
            if not api_key:
                print("ERROR: ANTHROPIC_API_KEY not set", file=sys.stderr)
                sys.exit(1)
            client = anthropic.Anthropic(api_key=api_key)
        except ImportError:
            print(
                "ERROR: anthropic package not installed. Run: pip install anthropic",
                file=sys.stderr,
            )
            sys.exit(1)

    data_path = Path(__file__).parent.parent / "data" / "sample_data.jsonl"
    tests: dict[str, list] = defaultdict(list)

    with open(data_path) as f:
        for line in f:
            r = json.loads(line)
            tests[r["test_id"]].append(r)

    stat_results: dict[str, Classification] = {}
    llm_results: dict[str, Classification] = {}
    llm_latencies: list[float] = []

    for test_id, history in sorted(tests.items()):
        if stat_only:
            stat_results[test_id] = classify(test_id, history)
        if run_llm:
            t0 = time.perf_counter()
            llm_results[test_id] = classify_llm(test_id, history, client)
            llm_latencies.append(time.perf_counter() - t0)
            if stat_only:
                pass  # already done above
            else:
                stat_results[test_id] = classify(test_id, history)

    if not stat_only and run_llm:
        # populate stat_results if we took the llm-only path
        for test_id, history in sorted(tests.items()):
            stat_results[test_id] = classify(test_id, history)

    results = stat_results  # primary display

    # Summary
    label_counts = Counter(r.label for r in results.values())
    print("=== Label distribution (statistical) ===")
    for label, count in sorted(label_counts.items()):
        print(f"  {label}: {count}")

    # Borderline cases — where the statistical classifier is uncertain
    print("\n=== Low-confidence classifications (ambiguous cases) ===")
    low_conf = [(tid, r) for tid, r in results.items() if r.confidence < 0.7]
    low_conf.sort(key=lambda x: x[1].confidence)
    for tid, r in low_conf:
        s = r.signals
        print(f"  {tid}")
        print(
            f"    label={r.label} confidence={r.confidence} pass_rate={s['pass_rate']:.0%} "
            f"trend_drop={s['trend_drop']:.0%} error_diversity={s['error_diversity']:.2f}"
        )
        print(f"    {r.reasoning}")

    # Near-threshold cases — close to healthy/broken boundary
    print("\n=== Near-threshold cases (15-25% or 90-95% pass rate) ===")
    for tid, r in sorted(results.items(), key=lambda x: x[1].signals["pass_rate"]):
        pr = r.signals["pass_rate"]
        if pr < 0.25 or (0.90 <= pr <= 0.95):
            print(
                f"  {tid}: {r.label} pass={pr:.0%} conf={r.confidence} | {r.reasoning}"
            )

    # Duration anomalies
    print("\n=== Duration drift > 20% ===")
    for tid, r in sorted(
        results.items(), key=lambda x: x[1].signals["duration_drift"], reverse=True
    ):
        drift = r.signals["duration_drift"]
        if abs(drift) > 0.2:
            print(
                f"  {tid}: drift={drift:+.0%} label={r.label} pass={r.signals['pass_rate']:.0%}"
            )

    # Full results sorted by pass rate (to see the spectrum)
    print("\n=== Full results (sorted by pass rate) ===")
    print(
        f"  {'test_id':<45} {'label':<18} {'conf':>5} {'pass':>6}"
        f" {'trend':>7} {'div':>6} {'drift':>7}"
    )
    print(f"  {'-' * 45} {'-' * 18} {'-' * 5} {'-' * 6} {'-' * 7} {'-' * 6} {'-' * 7}")
    for tid, r in sorted(results.items(), key=lambda x: x[1].signals["pass_rate"]):
        s = r.signals
        print(
            f"  {tid:<45} {r.label:<18} {r.confidence:>5.2f} "
            f"{s['pass_rate']:>6.0%} {s['trend_drop']:>+7.0%} "
            f"{s['error_diversity']:>6.2f} {s['duration_drift']:>+7.0%}"
        )

    # ── LLM comparison report ─────────────────────────────────────────────────
    if run_llm and llm_results:
        agree = sum(
            1
            for tid in stat_results
            if stat_results[tid].label == llm_results[tid].label
        )
        total = len(stat_results)
        print(
            f"\n=== Classifier comparison: agreement {agree}/{total} ({agree / total:.0%}) ==="
        )

        divergent = [
            (tid, stat_results[tid], llm_results[tid])
            for tid in stat_results
            if stat_results[tid].label != llm_results[tid].label
        ]
        divergent.sort(key=lambda x: x[1].signals["pass_rate"])

        if divergent:
            print(f"\n  Divergent cases ({len(divergent)}):")
            print(f"  {'test_id':<45} {'stat_label':<18} {'llm_label':<18} {'pass':>6}")
            print(f"  {'-' * 45} {'-' * 18} {'-' * 18} {'-' * 6}")
            for tid, sr, lr in divergent:
                pr = sr.signals["pass_rate"]
                print(f"  {tid:<45} {sr.label:<18} {lr.label:<18} {pr:>6.0%}")
                print(f"    stat: {sr.reasoning}")
                print(f"    llm:  {lr.reasoning}")

        if llm_latencies:
            lats = sorted(llm_latencies)
            p50 = lats[len(lats) // 2]
            p95 = lats[int(len(lats) * 0.95)]
            print(
                f"\n  LLM latency  p50={p50 * 1000:.0f}ms"
                f"  p95={p95 * 1000:.0f}ms  total={sum(lats):.1f}s"
            )

        # Rough cost estimate: Haiku 4.5 @ $1/$5 per 1M tokens
        # System prompt ~300 tokens cached after first; summary ~150 tokens each
        n = len(llm_results)
        input_tokens_est = (
            300 + n * 150
        )  # first call uncached + rest cached + summaries
        output_tokens_est = n * 60
        cost_est = (input_tokens_est / 1e6 * 1.0) + (output_tokens_est / 1e6 * 5.0)
        print(
            f"  Estimated cost for {n} tests: ${cost_est:.4f} "
            f"(~{input_tokens_est} in, ~{output_tokens_est} out tokens, Haiku 4.5 rates)"
        )


if __name__ == "__main__":
    main()
