package main

import (
	"context"
	"errors"
	"flag"
	"log"
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
	flag.Parse()

	s, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	var c classifier.Classifier
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		log.Println("ANTHROPIC_API_KEY set — LLM classifier enabled")
		c = classifier.NewEnsemble(classifier.NewStatistical(), classifier.NewLLM(key))
	} else {
		log.Println("ANTHROPIC_API_KEY not set — using statistical classifier only")
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
		log.Printf("listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	if err := s.Close(); err != nil {
		log.Printf("store close: %v", err)
	}
	log.Println("stopped")
}
