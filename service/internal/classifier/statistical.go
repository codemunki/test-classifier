package classifier

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

const (
	window           = 50
	recentWindow     = 10
	healthyThreshold = 0.95
	brokenThreshold  = 0.15
	minRuns          = 5
	trendDrop        = 0.15
	durationDrift    = 0.30
	// A recent-window pass rate at or below this overrides a mid-range overall
	// pass rate to broken — catches tests that have recently collapsed.
	recentCollapse = 0.10
)

var (
	reURL    = regexp.MustCompile(`https?://\S+`)
	reMS     = regexp.MustCompile(`\d+ms`)
	reDigits = regexp.MustCompile(`\d+`)
)

type statistical struct{}

// NewStatistical returns a Classifier that uses windowed heuristics.
func NewStatistical() Classifier {
	return &statistical{}
}

func (s *statistical) Classify(_ context.Context, testID string, history []Run) (Result, error) {
	sort.Slice(history, func(i, j int) bool {
		return history[i].StartedAt.Before(history[j].StartedAt)
	})

	w := history
	if len(w) > window {
		w = w[len(w)-window:]
	}

	if len(w) < minRuns {
		return Result{
			Label:      LabelInsufficientData,
			Confidence: 1.0,
			Reasoning:  fmt.Sprintf("Only %d runs available, need at least %d.", len(w), minRuns),
		}, nil
	}

	nonSkipped := make([]Run, 0, len(w))
	for _, r := range w {
		if r.Status != "skipped" {
			nonSkipped = append(nonSkipped, r)
		}
	}
	if len(nonSkipped) == 0 {
		return Result{
			Label:      LabelInsufficientData,
			Confidence: 1.0,
			Reasoning:  "All runs in window were skipped.",
		}, nil
	}

	passRate := passRateOf(nonSkipped)

	recent := nonSkipped
	if len(recent) > recentWindow {
		recent = recent[len(recent)-recentWindow:]
	}
	historical := nonSkipped
	if len(nonSkipped) > recentWindow {
		historical = nonSkipped[:len(nonSkipped)-recentWindow]
	} else {
		historical = nil
	}

	recentPR := passRateOf(recent)
	historicalPR := passRate
	if len(historical) > 0 {
		historicalPR = passRateOf(historical)
	}
	drop := historicalPR - recentPR

	failures := onlyFailed(nonSkipped)
	errorDiversity := computeErrorDiversity(failures)

	driftRatio := computeDurationDrift(w)

	// Recent collapse: overall pass rate is above broken threshold but the test
	// has effectively stopped passing in the recent window.
	if passRate >= brokenThreshold && recentPR <= recentCollapse && len(recent) >= recentWindow {
		return Result{
			Label:      LabelBroken,
			Confidence: 0.85,
			Reasoning: fmt.Sprintf(
				"Pass rate collapsed to %.0f%% in last %d runs (overall %.0f%%).",
				recentPR*100, recentWindow, passRate*100,
			),
		}, nil
	}

	if passRate > healthyThreshold && drop < trendDrop && driftRatio < durationDrift {
		confidence := math.Min(1.0, 0.7+(passRate-healthyThreshold)*6)
		return Result{
			Label:      LabelHealthy,
			Confidence: round2(confidence),
			Reasoning:  fmt.Sprintf("Pass rate %.0f%% over %d runs. No negative trend.", passRate*100, len(w)),
		}, nil
	}

	if passRate < brokenThreshold {
		confidence := math.Min(1.0, 0.7+(brokenThreshold-passRate)*6)
		qualifier := "Errors are varied."
		if errorDiversity < 0.4 {
			qualifier = "Dominant error pattern."
		}
		return Result{
			Label:      LabelBroken,
			Confidence: round2(confidence),
			Reasoning:  fmt.Sprintf("Pass rate %.0f%% over %d runs. %s", passRate*100, len(w), qualifier),
		}, nil
	}

	if passRate > healthyThreshold && drop >= trendDrop {
		return Result{
			Label:      LabelDegrading,
			Confidence: 0.70,
			Reasoning: fmt.Sprintf(
				"Pass rate still %.0f%% but dropped %.0f%% recently. Duration drift %+.0f%%.",
				passRate*100, drop*100, driftRatio*100,
			),
		}, nil
	}

	if driftRatio > durationDrift && passRate > 0.7 {
		return Result{
			Label:      LabelDegrading,
			Confidence: 0.65,
			Reasoning: fmt.Sprintf(
				"Pass rate %.0f%% but p50 duration up %.0f%% in recent runs.",
				passRate*100, driftRatio*100,
			),
		}, nil
	}

	// Flaky — characterise by signal pattern
	confidence := 0.5 + math.Abs(0.5-passRate)
	var flavour string
	switch {
	case errorDiversity < 0.4:
		flavour = "Consistent failure pattern — may indicate a recurring environment issue."
	case passRate < 0.35:
		flavour = "Severe failure rate with varied errors — borderline broken."
	default:
		flavour = "Intermittent failures with varied errors."
	}
	return Result{
		Label:      LabelFlaky,
		Confidence: round2(confidence),
		Reasoning:  fmt.Sprintf("Pass rate %.0f%% over %d runs. %s", passRate*100, len(w), flavour),
	}, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func passRateOf(runs []Run) float64 {
	if len(runs) == 0 {
		return 0
	}
	var passed int
	for _, r := range runs {
		if r.Status == "passed" {
			passed++
		}
	}
	return float64(passed) / float64(len(runs))
}

func onlyFailed(runs []Run) []Run {
	out := make([]Run, 0, len(runs))
	for _, r := range runs {
		if r.Status == "failed" || r.Status == "errored" {
			out = append(out, r)
		}
	}
	return out
}

func normalizeError(msg string) string {
	msg = strings.ToLower(msg)
	msg = reURL.ReplaceAllString(msg, "<url>")
	msg = reMS.ReplaceAllString(msg, "<ms>")
	msg = reDigits.ReplaceAllString(msg, "<n>")
	return strings.TrimSpace(msg)
}

func computeErrorDiversity(failures []Run) float64 {
	if len(failures) == 0 {
		return 0
	}
	seen := make(map[string]struct{})
	for _, r := range failures {
		if r.ErrorMessage != "" {
			seen[normalizeError(r.ErrorMessage)] = struct{}{}
		}
	}
	return float64(len(seen)) / float64(len(failures))
}

func computeDurationDrift(runs []Run) float64 {
	durations := make([]float64, 0, len(runs))
	for _, r := range runs {
		if r.DurationMS > 0 {
			durations = append(durations, float64(r.DurationMS))
		}
	}
	if len(durations) < 4 {
		return 0
	}
	half := len(durations) / 2
	earlyMed := median(durations[:half])
	lateMed := median(durations[half:])
	if earlyMed == 0 {
		return 0
	}
	return (lateMed - earlyMed) / earlyMed
}

func median(vals []float64) float64 {
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
