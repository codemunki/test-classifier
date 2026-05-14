package classifier

import (
	"context"
	"log/slog"
)

const ensembleThreshold = 0.70

type ensemble struct {
	stat Classifier
	llm  Classifier
}

// NewEnsemble returns a Classifier that uses stat for high-confidence cases
// and escalates to llm when confidence falls below ensembleThreshold.
func NewEnsemble(stat, llm Classifier) Classifier {
	return &ensemble{stat: stat, llm: llm}
}

func (e *ensemble) Classify(ctx context.Context, testID string, history []Run) (Result, error) {
	result, err := e.stat.Classify(ctx, testID, history)
	if err != nil {
		return Result{}, err
	}

	if result.Confidence >= ensembleThreshold {
		return result, nil
	}

	slog.Debug("escalating to LLM",
		"test_id", testID,
		"confidence", result.Confidence,
		"threshold", ensembleThreshold)

	llmResult, err := e.llm.Classify(ctx, testID, history)
	if err != nil {
		// LLM failure is non-fatal — fall back to the statistical result
		slog.Warn("LLM classifier failed, using stat result", "test_id", testID, "err", err)
		return result, nil
	}

	return llmResult, nil
}
