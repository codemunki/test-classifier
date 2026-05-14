package classifier

import (
	"context"
	"log"
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

	log.Printf("test %q: stat confidence %.2f < %.2f, escalating to LLM", testID, result.Confidence, ensembleThreshold)

	llmResult, err := e.llm.Classify(ctx, testID, history)
	if err != nil {
		// LLM failure is non-fatal — fall back to the statistical result
		log.Printf("test %q: LLM classifier failed (%v), using stat result", testID, err)
		return result, nil
	}

	return llmResult, nil
}
