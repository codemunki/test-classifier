package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Haiku 4.5 pricing (USD per 1M tokens).
const (
	haiku45InputPricePerM  = 1.00
	haiku45OutputPricePerM = 5.00
)

const llmModel = anthropic.ModelClaudeHaiku4_5

const llmSystemPrompt = `You classify automated test health from run history.

Categories:
- healthy: pass rate >95% in recent window, no negative trend
- flaky: pass rate 15-95%, or inconsistent failures
- broken: pass rate <15%, or 0% in the most recent runs despite higher overall rate
- degrading: acceptable pass rate but duration increasing significantly
- insufficient_data: fewer than 5 runs

Respond with ONLY the JSON object below — no markdown fences, no explanations, no notes:
{"label": "<category>", "confidence": <0.0-1.0>, "reasoning": "<one sentence>"}

Rules:
- label must be exactly one of the five categories above
- confidence is your certainty about the label (not the test's reliability)
- reasoning is one clear sentence citing the key signal
- if the data suggests a category not in the list, pick the closest valid one`

var llmValidLabels = map[Label]bool{
	LabelHealthy:          true,
	LabelFlaky:            true,
	LabelBroken:           true,
	LabelDegrading:        true,
	LabelInsufficientData: true,
}

type llmClassifier struct {
	client *anthropic.Client
}

// NewLLM returns a Classifier that delegates to Claude.
// The system prompt is marked for prompt caching to reduce cost when
// classifying many tests in sequence.
func NewLLM(apiKey string) Classifier {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &llmClassifier{client: &client}
}

func (l *llmClassifier) Classify(ctx context.Context, testID string, history []Run) (Result, error) {
	summary := buildSummary(testID, history)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	start := time.Now()
	msg, err := l.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 256,
		System: []anthropic.TextBlockParam{
			{
				Text:         llmSystemPrompt,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(summary)),
		},
	})
	latency := time.Since(start)
	if err != nil {
		return Result{}, fmt.Errorf("llm classify: %w", err)
	}

	inTokens := msg.Usage.InputTokens
	outTokens := msg.Usage.OutputTokens
	costUSD := (float64(inTokens)/1e6)*haiku45InputPricePerM +
		(float64(outTokens)/1e6)*haiku45OutputPricePerM
	slog.Info("llm",
		"test_id", testID,
		"duration_ms", latency.Milliseconds(),
		"input_tokens", inTokens,
		"output_tokens", outTokens,
		"cost_usd", costUSD)

	if len(msg.Content) == 0 {
		return Result{}, fmt.Errorf("llm returned empty response")
	}

	raw := strings.TrimSpace(msg.Content[0].Text)
	slog.Debug("llm response", "test_id", testID, "raw", raw)

	// Extract the last JSON object — handles markdown fences and self-correction
	// patterns where the model appends a second, corrected JSON block.
	jsonStart := strings.LastIndex(raw, "{")
	jsonEnd := strings.LastIndex(raw, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd < jsonStart {
		return Result{}, fmt.Errorf("llm response: no JSON object found")
	}
	raw = raw[jsonStart : jsonEnd+1]

	var out struct {
		Label      string  `json:"label"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Result{}, fmt.Errorf("llm response parse: %w", err)
	}

	label := Label(strings.TrimSpace(out.Label))
	if !llmValidLabels[label] {
		label = LabelFlaky
	}

	result := Result{
		Label:      label,
		Confidence: round2(out.Confidence),
		Reasoning:  out.Reasoning,
	}
	slog.Debug("llm parsed", "test_id", testID, "label", result.Label, "confidence", result.Confidence, "reasoning", result.Reasoning)
	return result, nil
}

// buildSummary produces a compact text summary of the run window for the LLM.
func buildSummary(testID string, history []Run) string {
	if len(history) == 0 {
		return fmt.Sprintf("test_id: %s\nruns_in_window: 0", testID)
	}

	w := history
	if len(w) > window {
		w = w[len(w)-window:]
	}

	nonSkipped := make([]Run, 0, len(w))
	for _, r := range w {
		if r.Status != "skipped" {
			nonSkipped = append(nonSkipped, r)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "test_id: %s\n", testID)
	fmt.Fprintf(&b, "runs_in_window: %d\n", len(w))

	if len(nonSkipped) == 0 {
		b.WriteString("all_runs_skipped: true\n")
		return b.String()
	}

	passed := 0
	for _, r := range nonSkipped {
		if r.Status == "passed" {
			passed++
		}
	}
	fmt.Fprintf(&b, "pass_rate: %.2f (%d/%d)\n", float64(passed)/float64(len(nonSkipped)), passed, len(nonSkipped))

	recent := nonSkipped
	if len(recent) > recentWindow {
		recent = recent[len(recent)-recentWindow:]
	}
	if len(nonSkipped) > recentWindow {
		old := nonSkipped[:len(nonSkipped)-recentWindow]
		recentPassed := 0
		for _, r := range recent {
			if r.Status == "passed" {
				recentPassed++
			}
		}
		oldPassed := 0
		for _, r := range old {
			if r.Status == "passed" {
				oldPassed++
			}
		}
		fmt.Fprintf(&b, "recent_%d_pass_rate: %.2f\n", recentWindow, float64(recentPassed)/float64(len(recent)))
		fmt.Fprintf(&b, "older_pass_rate: %.2f\n", float64(oldPassed)/float64(len(old)))
	}

	durations := make([]float64, 0, len(w))
	for _, r := range w {
		if r.DurationMS > 0 {
			durations = append(durations, float64(r.DurationMS))
		}
	}
	if len(durations) >= 4 {
		half := len(durations) / 2
		earlyMed := median(durations[:half])
		lateMed := median(durations[half:])
		if earlyMed > 0 {
			drift := (lateMed - earlyMed) / earlyMed
			fmt.Fprintf(&b, "duration_drift: %+.0f%% (early_p50=%.0fms, late_p50=%.0fms)\n",
				drift*100, earlyMed, lateMed)
		}
	}

	failures := onlyFailed(nonSkipped)
	if len(failures) > 0 {
		fmt.Fprintf(&b, "failure_count: %d\n", len(failures))
	}

	// Chronological timeline — lets the LLM spot temporal patterns (periodicity,
	// error drift, sudden state changes) that aggregated stats erase.
	b.WriteString("timeline (oldest→newest, skipped omitted):\n")
	const maxErrLen = 60
	for i, r := range w {
		if r.Status == "skipped" {
			continue
		}
		switch r.Status {
		case "passed":
			fmt.Fprintf(&b, "  [%d] passed %dms\n", i+1, r.DurationMS)
		default:
			msg := r.ErrorMessage
			if len(msg) > maxErrLen {
				msg = msg[:maxErrLen] + "…"
			}
			if msg != "" {
				fmt.Fprintf(&b, "  [%d] %s %dms %q\n", i+1, r.Status, r.DurationMS, msg)
			} else {
				fmt.Fprintf(&b, "  [%d] %s %dms\n", i+1, r.Status, r.DurationMS)
			}
		}
	}

	return b.String()
}
