package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mdoherty/test-classifier/internal/classifier"
	"github.com/mdoherty/test-classifier/internal/handler"
	"github.com/mdoherty/test-classifier/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "classifier.db", "SQLite database path")
	logLevel := flag.String("log-level", getEnvOr("LOG_LEVEL", "info"), "log level: debug, info, warn, error (overrides $LOG_LEVEL)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(*logLevel),
	})))

	s, err := store.Open(*dbPath)
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}

	var c classifier.Classifier
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		slog.Info("LLM classifier enabled")
		c = classifier.NewEnsemble(classifier.NewStatistical(), classifier.NewLLM(key))
	} else {
		slog.Info("statistical classifier only", "reason", "ANTHROPIC_API_KEY not set")
		c = classifier.NewStatistical()
	}

	srv := &http.Server{
		Addr:         *addr,
		Handler:      handler.New(s, c),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second, // generous for LLM path
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "err", err)
		os.Exit(1)
	}
	if err := s.Close(); err != nil {
		slog.Warn("store close", "err", err)
	}
	slog.Info("stopped")
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
