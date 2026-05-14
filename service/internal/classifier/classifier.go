package classifier

import (
	"context"
	"time"
)

// Label is the classification outcome for a test.
type Label string

const (
	LabelHealthy          Label = "healthy"
	LabelFlaky            Label = "flaky"
	LabelBroken           Label = "broken"
	LabelDegrading        Label = "degrading"
	LabelInsufficientData Label = "insufficient_data"
)

// Run is a single test execution record.
type Run struct {
	TestID       string
	RunID        string
	Status       string // passed | failed | skipped | errored
	DurationMS   int64
	StartedAt    time.Time
	ErrorMessage string
}

// Result is the classification output.
type Result struct {
	Label      Label   `json:"label"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// Classifier classifies a test given its run history.
type Classifier interface {
	Classify(ctx context.Context, testID string, history []Run) (Result, error)
}
