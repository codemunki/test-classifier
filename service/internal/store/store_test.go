package store_test

import (
	"testing"
	"time"

	"github.com/mdoherty/test-classifier/internal/classifier"
	"github.com/mdoherty/test-classifier/internal/store"
)

func openMemory(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertAndRetrieve(t *testing.T) {
	s := openMemory(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	runs := []classifier.Run{
		{TestID: "login.happy_path", RunID: "run_1", Status: "passed", DurationMS: 800, StartedAt: base},
		{TestID: "login.happy_path", RunID: "run_2", Status: "failed", DurationMS: 900, StartedAt: base.Add(time.Hour), ErrorMessage: "timeout"},
		{TestID: "other.test", RunID: "run_3", Status: "passed", DurationMS: 500, StartedAt: base},
	}
	for _, r := range runs {
		if err := s.InsertRun(r); err != nil {
			t.Fatalf("InsertRun: %v", err)
		}
	}

	history, err := s.GetHistory("login.happy_path", 50)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("got %d runs, want 2", len(history))
	}
	if history[0].RunID != "run_1" || history[1].RunID != "run_2" {
		t.Errorf("unexpected order: %v", history)
	}
	if history[1].ErrorMessage != "timeout" {
		t.Errorf("error_message not preserved: %q", history[1].ErrorMessage)
	}
}

func TestDuplicateRunIgnored(t *testing.T) {
	s := openMemory(t)
	r := classifier.Run{TestID: "t", RunID: "run_1", Status: "passed", StartedAt: time.Now()}

	if err := s.InsertRun(r); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	r.Status = "failed"
	if err := s.InsertRun(r); err != nil {
		t.Fatalf("second insert: %v", err)
	}

	history, _ := s.GetHistory("t", 10)
	if len(history) != 1 {
		t.Errorf("got %d runs, want 1 (duplicate should be ignored)", len(history))
	}
	if history[0].Status != "passed" {
		t.Errorf("original record was overwritten")
	}
}

func TestGetHistoryUnknownTest(t *testing.T) {
	s := openMemory(t)
	history, err := s.GetHistory("nonexistent", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected empty history, got %d runs", len(history))
	}
}

func TestGetHistoryLimit(t *testing.T) {
	s := openMemory(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 10 {
		s.InsertRun(classifier.Run{
			TestID:    "t",
			RunID:     "run_" + string(rune('a'+i)),
			Status:    "passed",
			StartedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}

	history, err := s.GetHistory("t", 5)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 5 {
		t.Errorf("got %d runs, want 5", len(history))
	}
}

func TestTestIDs(t *testing.T) {
	s := openMemory(t)
	base := time.Now()
	s.InsertRun(classifier.Run{TestID: "alpha", RunID: "r1", Status: "passed", StartedAt: base})
	s.InsertRun(classifier.Run{TestID: "beta", RunID: "r2", Status: "passed", StartedAt: base})
	s.InsertRun(classifier.Run{TestID: "alpha", RunID: "r3", Status: "failed", StartedAt: base.Add(time.Hour)})

	ids, err := s.TestIDs()
	if err != nil {
		t.Fatalf("TestIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("got %d ids, want 2: %v", len(ids), ids)
	}
}
