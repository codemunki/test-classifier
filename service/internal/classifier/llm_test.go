package classifier

import (
	"strings"
	"testing"
	"time"
)

func makeRun(id int, status string, durationMS int64, errMsg string) Run {
	return Run{
		TestID:       "t",
		RunID:        "r" + string(rune('0'+id)),
		Status:       status,
		DurationMS:   durationMS,
		StartedAt:    time.Date(2026, 1, id, 0, 0, 0, 0, time.UTC),
		ErrorMessage: errMsg,
	}
}

func TestBuildSummary_EmptyHistory(t *testing.T) {
	out := buildSummary("my.test", nil)
	if !strings.Contains(out, "test_id: my.test") {
		t.Errorf("missing test_id: %q", out)
	}
	if !strings.Contains(out, "runs_in_window: 0") {
		t.Errorf("missing runs_in_window: %q", out)
	}
	if strings.Contains(out, "pass_rate") {
		t.Errorf("pass_rate should not appear for empty history: %q", out)
	}
}

func TestBuildSummary_AllSkipped(t *testing.T) {
	history := []Run{
		makeRun(1, "skipped", 0, ""),
		makeRun(2, "skipped", 0, ""),
		makeRun(3, "skipped", 0, ""),
	}
	out := buildSummary("t", history)
	if !strings.Contains(out, "all_runs_skipped: true") {
		t.Errorf("expected all_runs_skipped: %q", out)
	}
	if strings.Contains(out, "pass_rate") {
		t.Errorf("pass_rate should not appear when all runs skipped: %q", out)
	}
}

func TestBuildSummary_PassRateIncluded(t *testing.T) {
	history := []Run{
		makeRun(1, "passed", 100, ""),
		makeRun(2, "passed", 100, ""),
		makeRun(3, "failed", 100, "timeout"),
		makeRun(4, "passed", 100, ""),
		makeRun(5, "passed", 100, ""),
	}
	out := buildSummary("t", history)
	// 4/5 = 0.80
	if !strings.Contains(out, "pass_rate: 0.80 (4/5)") {
		t.Errorf("unexpected pass_rate line: %q", out)
	}
}

func TestBuildSummary_RecentAndOlderRates_OnlyWhenAboveRecentWindow(t *testing.T) {
	// Fewer than recentWindow (10) non-skipped runs → no recent_N / older lines.
	history := []Run{
		makeRun(1, "passed", 100, ""),
		makeRun(2, "failed", 100, "err"),
		makeRun(3, "passed", 100, ""),
	}
	out := buildSummary("t", history)
	if strings.Contains(out, "recent_10_pass_rate") {
		t.Errorf("recent rate should not appear with < 10 non-skipped runs: %q", out)
	}
}

func TestBuildSummary_RecentAndOlderRates_WhenAboveRecentWindow(t *testing.T) {
	// 20 runs: first 10 all fail, last 10 all pass.
	history := make([]Run, 20)
	for i := range history {
		status := "failed"
		if i >= 10 {
			status = "passed"
		}
		history[i] = makeRun(i+1, status, 200, "assertion error")
	}
	out := buildSummary("t", history)
	if !strings.Contains(out, "recent_10_pass_rate: 1.00") {
		t.Errorf("expected recent pass rate 1.00: %q", out)
	}
	if !strings.Contains(out, "older_pass_rate: 0.00") {
		t.Errorf("expected older pass rate 0.00: %q", out)
	}
}

func TestBuildSummary_DurationDrift_AppearsWhenFourOrMoreDurations(t *testing.T) {
	// 8 runs: early 4 at 100ms, late 4 at 300ms → drift = +200%.
	history := []Run{
		makeRun(1, "passed", 100, ""),
		makeRun(2, "passed", 100, ""),
		makeRun(3, "passed", 100, ""),
		makeRun(4, "passed", 100, ""),
		makeRun(5, "passed", 300, ""),
		makeRun(6, "passed", 300, ""),
		makeRun(7, "passed", 300, ""),
		makeRun(8, "passed", 300, ""),
	}
	out := buildSummary("t", history)
	if !strings.Contains(out, "duration_drift:") {
		t.Errorf("expected duration_drift line: %q", out)
	}
}

func TestBuildSummary_DurationDrift_AbsentWhenFewerThanFour(t *testing.T) {
	history := []Run{
		makeRun(1, "passed", 100, ""),
		makeRun(2, "passed", 200, ""),
		makeRun(3, "passed", 300, ""),
	}
	out := buildSummary("t", history)
	if strings.Contains(out, "duration_drift:") {
		t.Errorf("duration_drift should not appear with < 4 durations: %q", out)
	}
}

func TestBuildSummary_FailureCount(t *testing.T) {
	history := []Run{
		makeRun(1, "passed", 100, ""),
		makeRun(2, "failed", 100, "err1"),
		makeRun(3, "errored", 100, "err2"),
	}
	out := buildSummary("t", history)
	if !strings.Contains(out, "failure_count: 2") {
		t.Errorf("expected failure_count: 2: %q", out)
	}
}

func TestBuildSummary_Timeline_SkippedRunsOmitted(t *testing.T) {
	history := []Run{
		makeRun(1, "passed", 100, ""),
		makeRun(2, "skipped", 0, ""),
		makeRun(3, "failed", 200, "boom"),
	}
	out := buildSummary("t", history)
	if !strings.Contains(out, "timeline") {
		t.Fatalf("no timeline section: %q", out)
	}
	// The header says "skipped omitted" — check that no timeline entry exists for
	// the skipped run (which was at window index 2, so would print as "[2]").
	if strings.Contains(out, "[2]") {
		t.Errorf("skipped run at index 2 should not appear as a timeline entry: %q", out)
	}
	if !strings.Contains(out, "passed 100ms") {
		t.Errorf("passed run missing from timeline: %q", out)
	}
	if !strings.Contains(out, `"boom"`) {
		t.Errorf("failed run error missing from timeline: %q", out)
	}
}

func TestBuildSummary_Timeline_LongErrorTruncated(t *testing.T) {
	longMsg := strings.Repeat("x", 80)
	history := []Run{makeRun(1, "failed", 100, longMsg)}
	out := buildSummary("t", history)
	if !strings.Contains(out, "…") {
		t.Errorf("long error message should be truncated with ellipsis: %q", out)
	}
	// The truncated message in the timeline should be 60 chars + "…", not the full 80.
	if strings.Contains(out, longMsg) {
		t.Errorf("full long error message should not appear verbatim: %q", out)
	}
}

func TestBuildSummary_WindowTruncation(t *testing.T) {
	// 60 runs; only the last 50 should appear in runs_in_window.
	history := make([]Run, 60)
	for i := range history {
		history[i] = makeRun(i%28+1, "passed", 100, "")
		history[i].StartedAt = time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC)
	}
	out := buildSummary("t", history)
	if !strings.Contains(out, "runs_in_window: 50") {
		t.Errorf("expected window capped at 50: %q", out)
	}
}
