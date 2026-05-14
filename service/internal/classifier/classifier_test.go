package classifier_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mdoherty/test-classifier/internal/classifier"
)

// makeRuns builds a slice of runs with the given statuses, newest last.
// statuses is a string of characters: 'p'=passed, 'f'=failed, 'e'=errored, 's'=skipped.
func makeRuns(statuses string) []classifier.Run {
	runs := make([]classifier.Run, len(statuses))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, ch := range statuses {
		var status string
		switch ch {
		case 'p':
			status = "passed"
		case 'f':
			status = "failed"
		case 'e':
			status = "errored"
		case 's':
			status = "skipped"
		}
		runs[i] = classifier.Run{
			TestID:     "test_bdd",
			RunID:      fmt.Sprintf("run_%d", i),
			Status:     status,
			DurationMS: 1000,
			StartedAt:  base.Add(time.Duration(i) * time.Hour),
		}
	}
	return runs
}

// makeRunsWithDrift builds runs where recent durations are inflated by the given multiplier.
func makeRunsWithDrift(n int, driftMultiplier float64) []classifier.Run {
	runs := make([]classifier.Run, n)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	half := n / 2
	for i := range runs {
		dur := int64(1000)
		if i >= half {
			dur = int64(float64(dur) * driftMultiplier)
		}
		runs[i] = classifier.Run{
			TestID:     "test_bdd",
			RunID:      fmt.Sprintf("run_%d", i),
			Status:     "passed",
			DurationMS: dur,
			StartedAt:  base.Add(time.Duration(i) * time.Hour),
		}
	}
	return runs
}

// scenario is a BDD test case.
type scenario struct {
	name          string
	history       []classifier.Run
	wantLabel     classifier.Label
	minConfidence float64
}

var scenarios = []scenario{
	{
		// Given 48/50 recent runs passing with stable duration → healthy
		name:          "healthy: high pass rate, no trend",
		history:       makeRuns("pp" + repeat('p', 48)),
		wantLabel:     classifier.LabelHealthy,
		minConfidence: 0.9,
	},
	{
		// Given ~50% pass rate with interleaved failures → flaky
		// (not front-loaded: the recent window must not look like a collapse)
		name:          "flaky: intermittent failures",
		history:       makeRuns(repeatStr("pf", 25)),
		wantLabel:     classifier.LabelFlaky,
		minConfidence: 0.5,
	},
	{
		// Given <15% pass rate over last 50 runs → broken
		name:          "broken: very low pass rate",
		history:       makeRuns(repeat('f', 47) + "ppp"),
		wantLabel:     classifier.LabelBroken,
		minConfidence: 0.7,
	},
	{
		// Given fewer than 5 runs → insufficient_data
		name:          "insufficient_data: too few runs",
		history:       makeRuns("pppp"),
		wantLabel:     classifier.LabelInsufficientData,
		minConfidence: 1.0,
	},
	{
		// Given all runs skipped → insufficient_data
		name:          "insufficient_data: all skipped",
		history:       makeRuns(repeat('s', 20)),
		wantLabel:     classifier.LabelInsufficientData,
		minConfidence: 1.0,
	},
	{
		// Given good historical pass rate but 0% in the last 10 runs → broken
		// (the recent-collapse pattern uncovered by the prototype comparison)
		name:          "broken: recent collapse despite good history",
		history:       makeRuns(repeat('p', 40) + repeat('f', 10)),
		wantLabel:     classifier.LabelBroken,
		minConfidence: 0.7,
	},
	{
		// Given 100% pass rate but significant duration drift → degrading
		name:          "degrading: duration drift on passing test",
		history:       makeRunsWithDrift(50, 2.0),
		wantLabel:     classifier.LabelDegrading,
		minConfidence: 0.6,
	},
}

func repeat(ch rune, n int) string {
	s := make([]rune, n)
	for i := range s {
		s[i] = ch
	}
	return string(s)
}

func repeatStr(s string, n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(s)
	}
	return b.String()
}

// TestStatisticalClassifier runs all BDD scenarios against the statistical classifier.
func TestStatisticalClassifier(t *testing.T) {
	c := classifier.NewStatistical()
	runScenarios(t, c)
}

func runScenarios(t *testing.T, c classifier.Classifier) {
	t.Helper()
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			result, err := c.Classify(context.Background(), "test_bdd", sc.history)
			if err != nil {
				t.Fatalf("Classify returned error: %v", err)
			}
			if result.Label != sc.wantLabel {
				t.Errorf("label = %q, want %q (confidence=%.2f, reasoning=%q)",
					result.Label, sc.wantLabel, result.Confidence, result.Reasoning)
			}
			if result.Confidence < sc.minConfidence {
				t.Errorf("confidence = %.2f, want >= %.2f", result.Confidence, sc.minConfidence)
			}
		})
	}
}
