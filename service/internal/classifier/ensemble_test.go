package classifier_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mdoherty/test-classifier/internal/classifier"
)

// fixed is a stub Classifier that returns a preset result or error.
type fixed struct {
	result classifier.Result
	err    error
	called int
}

func (f *fixed) Classify(_ context.Context, _ string, _ []classifier.Run) (classifier.Result, error) {
	f.called++
	return f.result, f.err
}

var stubHistory = []classifier.Run{} // content irrelevant for ensemble unit tests

func TestEnsemble_HighConfidence_NoLLMCall(t *testing.T) {
	stat := &fixed{result: classifier.Result{Label: classifier.LabelHealthy, Confidence: 0.90, Reasoning: "all good"}}
	llm := &fixed{result: classifier.Result{Label: classifier.LabelFlaky, Confidence: 0.80}}

	c := classifier.NewEnsemble(stat, llm)
	res, err := c.Classify(context.Background(), "t", stubHistory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Label != classifier.LabelHealthy {
		t.Errorf("label = %q, want healthy", res.Label)
	}
	if llm.called != 0 {
		t.Errorf("LLM was called %d times, want 0 (stat confidence was above threshold)", llm.called)
	}
}

func TestEnsemble_LowConfidence_EscalatesToLLM(t *testing.T) {
	stat := &fixed{result: classifier.Result{Label: classifier.LabelFlaky, Confidence: 0.60}}
	llm := &fixed{result: classifier.Result{Label: classifier.LabelBroken, Confidence: 0.85, Reasoning: "recent collapse"}}

	c := classifier.NewEnsemble(stat, llm)
	res, err := c.Classify(context.Background(), "t", stubHistory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Label != classifier.LabelBroken {
		t.Errorf("label = %q, want broken (LLM result)", res.Label)
	}
	if llm.called != 1 {
		t.Errorf("LLM called %d times, want 1", llm.called)
	}
}

func TestEnsemble_ExactThreshold_NoEscalation(t *testing.T) {
	// Confidence exactly at threshold (0.70) should NOT escalate.
	stat := &fixed{result: classifier.Result{Label: classifier.LabelFlaky, Confidence: 0.70}}
	llm := &fixed{}

	c := classifier.NewEnsemble(stat, llm)
	res, err := c.Classify(context.Background(), "t", stubHistory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Label != classifier.LabelFlaky {
		t.Errorf("label = %q, want flaky", res.Label)
	}
	if llm.called != 0 {
		t.Errorf("LLM called %d times, want 0 (at-threshold should not escalate)", llm.called)
	}
}

func TestEnsemble_LLMFailure_FallsBackToStat(t *testing.T) {
	statResult := classifier.Result{Label: classifier.LabelFlaky, Confidence: 0.60, Reasoning: "intermittent"}
	stat := &fixed{result: statResult}
	llm := &fixed{err: errors.New("API timeout")}

	c := classifier.NewEnsemble(stat, llm)
	res, err := c.Classify(context.Background(), "t", stubHistory)
	if err != nil {
		t.Fatalf("unexpected error: LLM failure should be non-fatal, got %v", err)
	}
	if res.Label != classifier.LabelFlaky {
		t.Errorf("label = %q, want flaky (stat fallback)", res.Label)
	}
	if res.Reasoning != statResult.Reasoning {
		t.Errorf("reasoning = %q, want stat reasoning %q", res.Reasoning, statResult.Reasoning)
	}
}

func TestEnsemble_StatFailure_ReturnsError(t *testing.T) {
	stat := &fixed{err: errors.New("stat error")}
	llm := &fixed{}

	c := classifier.NewEnsemble(stat, llm)
	_, err := c.Classify(context.Background(), "t", stubHistory)
	if err == nil {
		t.Fatal("expected error when stat classifier fails, got nil")
	}
	if llm.called != 0 {
		t.Errorf("LLM called %d times after stat failure, want 0", llm.called)
	}
}
