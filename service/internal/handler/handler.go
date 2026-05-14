package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mdoherty/test-classifier/internal/classifier"
	"github.com/mdoherty/test-classifier/internal/store"
)

const (
	maxBodyBytes       = 1 << 20 // 1 MiB
	maxTestIDLen       = 256
	maxRunIDLen        = 256
	maxErrorMessageLen = 4096
)

// Server holds the dependencies for the HTTP handlers.
type Server struct {
	store      *store.Store
	classifier classifier.Classifier
}

// New returns an HTTP handler with both routes registered.
func New(s *store.Store, c classifier.Classifier) http.Handler {
	srv := &Server{store: s, classifier: c}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", srv.handleIngest)
	mux.HandleFunc("GET /tests/{test_id}", srv.handleClassify)
	return LoggingMiddleware(mux)
}

// ── POST /events ──────────────────────────────────────────────────────────────

type eventRequest struct {
	TestID       string `json:"test_id"`
	RunID        string `json:"run_id"`
	Status       string `json:"status"`
	DurationMS   int64  `json:"duration_ms"`
	StartedAt    string `json:"started_at"`
	ErrorMessage string `json:"error_message"`
}

var validStatuses = map[string]bool{
	"passed": true, "failed": true, "skipped": true, "errored": true,
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req eventRequest
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}

	if err := validateEvent(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	startedAt, err := time.Parse(time.RFC3339, req.StartedAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "started_at must be an RFC3339 timestamp")
		return
	}

	// Truncate error_message rather than reject — callers may not control its length.
	errorMessage := req.ErrorMessage
	if len(errorMessage) > maxErrorMessageLen {
		errorMessage = errorMessage[:maxErrorMessageLen]
	}

	run := classifier.Run{
		TestID:       req.TestID,
		RunID:        req.RunID,
		Status:       req.Status,
		DurationMS:   req.DurationMS,
		StartedAt:    startedAt,
		ErrorMessage: errorMessage,
	}
	if err := s.store.InsertRun(run); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store run")
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func validateEvent(req eventRequest) error {
	if strings.TrimSpace(req.TestID) == "" {
		return errors.New("test_id is required")
	}
	if len(req.TestID) > maxTestIDLen {
		return fmt.Errorf("test_id exceeds %d characters", maxTestIDLen)
	}
	if strings.TrimSpace(req.RunID) == "" {
		return errors.New("run_id is required")
	}
	if len(req.RunID) > maxRunIDLen {
		return fmt.Errorf("run_id exceeds %d characters", maxRunIDLen)
	}
	if !validStatuses[req.Status] {
		return errors.New("status must be one of: passed, failed, skipped, errored")
	}
	if req.StartedAt == "" {
		return errors.New("started_at is required")
	}
	return nil
}

// ── GET /tests/{test_id} ──────────────────────────────────────────────────────

type classifyResponse struct {
	TestID     string           `json:"test_id"`
	Label      classifier.Label `json:"label"`
	Confidence float64          `json:"confidence"`
	Reasoning  string           `json:"reasoning"`
}

func (s *Server) handleClassify(w http.ResponseWriter, r *http.Request) {
	testID := r.PathValue("test_id")
	if strings.TrimSpace(testID) == "" || len(testID) > maxTestIDLen {
		writeError(w, http.StatusBadRequest, "invalid test_id")
		return
	}

	history, err := s.store.GetHistory(testID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve history")
		return
	}

	start := time.Now()
	result, err := s.classifier.Classify(r.Context(), testID, history)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "classification failed")
		return
	}
	slog.Info("classify",
		"test_id", testID,
		"label", result.Label,
		"confidence", result.Confidence,
		"duration_ms", time.Since(start).Milliseconds())

	writeJSON(w, http.StatusOK, classifyResponse{
		TestID:     testID,
		Label:      result.Label,
		Confidence: result.Confidence,
		Reasoning:  result.Reasoning,
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
