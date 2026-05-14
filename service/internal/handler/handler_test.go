package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdoherty/test-classifier/internal/classifier"
	"github.com/mdoherty/test-classifier/internal/handler"
	"github.com/mdoherty/test-classifier/internal/store"
)

// failClassifier always returns an error from Classify.
type failClassifier struct{}

func (f *failClassifier) Classify(_ context.Context, _ string, _ []classifier.Run) (classifier.Result, error) {
	return classifier.Result{}, errors.New("forced classifier error")
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return handler.New(s, classifier.NewStatistical())
}

func post(t *testing.T, h http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func get(t *testing.T, h http.Handler, testID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/tests/"+testID, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// ── POST /events ──────────────────────────────────────────────────────────────

func TestIngest_AcceptsValidEvent(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"test_id":     "login.happy_path",
		"run_id":      "run_1",
		"status":      "passed",
		"duration_ms": 800,
		"started_at":  "2026-01-01T00:00:00Z",
	})
	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202; body: %s", w.Code, w.Body)
	}
}

func TestIngest_RejectsMissingTestID(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"run_id": "run_1", "status": "passed", "started_at": "2026-01-01T00:00:00Z",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestIngest_RejectsInvalidStatus(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"test_id": "t", "run_id": "r1", "status": "unknown", "started_at": "2026-01-01T00:00:00Z",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestIngest_RejectsInvalidStartedAt(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"test_id": "t", "run_id": "r1", "status": "passed", "started_at": "not-a-date",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestIngest_RejectsBadJSON(t *testing.T) {
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestIngest_RejectsWrongContentType(t *testing.T) {
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", w.Code)
	}
}

func TestIngest_RejectsNegativeDuration(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"test_id": "t", "run_id": "r1", "status": "passed",
		"duration_ms": -1, "started_at": "2026-01-01T00:00:00Z",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestIngest_RejectsFutureTimestamp(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"test_id": "t", "run_id": "r1", "status": "passed",
		"started_at": "2099-01-01T00:00:00Z",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestIngest_RejectsInvalidTestIDChars(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"test_id": "bad\nid", "run_id": "r1", "status": "passed",
		"started_at": "2026-01-01T00:00:00Z",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestIngest_RejectsInvalidRunIDChars(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"test_id": "t", "run_id": "bad id", "status": "passed",
		"started_at": "2026-01-01T00:00:00Z",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestIngest_ErrorMessageClearedOnPassedRun(t *testing.T) {
	h := newTestServer(t)
	w := post(t, h, map[string]any{
		"test_id": "t", "run_id": "r1", "status": "passed",
		"started_at": "2026-01-01T00:00:00Z", "error_message": "should be cleared",
	})
	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202 (error_message on passed should be accepted silently)", w.Code)
	}
}

func TestIngest_DuplicateRunIsIdempotent(t *testing.T) {
	h := newTestServer(t)
	event := map[string]any{
		"test_id": "t", "run_id": "run_1", "status": "passed", "started_at": "2026-01-01T00:00:00Z",
	}
	post(t, h, event)
	w := post(t, h, event)
	if w.Code != http.StatusAccepted {
		t.Errorf("second insert status = %d, want 202", w.Code)
	}
}

// ── GET /tests/{test_id} ──────────────────────────────────────────────────────

func TestClassify_InsufficientDataForNewTest(t *testing.T) {
	h := newTestServer(t)
	w := get(t, h, "never.seen")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["label"] != "insufficient_data" {
		t.Errorf("label = %q, want insufficient_data", resp["label"])
	}
}

func TestClassify_ClassifierError_Returns500(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	h := handler.New(s, &failClassifier{})
	w := get(t, h, "some-test")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestClassify_StoreError_Returns500(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	h := handler.New(s, classifier.NewStatistical())
	s.Close() // force GetHistory to fail

	w := get(t, h, "some-test")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestClassify_ReturnsLabelAfterIngestion(t *testing.T) {
	h := newTestServer(t)

	// Ingest 50 passing runs
	for i := range 50 {
		post(t, h, map[string]any{
			"test_id":     "cart.happy_path",
			"run_id":      "run_" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			"status":      "passed",
			"duration_ms": 500,
			"started_at":  "2026-01-01T00:00:00Z",
		})
	}

	w := get(t, h, "cart.happy_path")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["label"] != string(classifier.LabelHealthy) {
		t.Errorf("label = %q, want healthy", resp["label"])
	}
	if resp["test_id"] != "cart.happy_path" {
		t.Errorf("test_id = %q, want cart.happy_path", resp["test_id"])
	}
	if resp["confidence"] == nil || resp["reasoning"] == nil {
		t.Error("response missing confidence or reasoning")
	}
}
