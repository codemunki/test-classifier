BIN       := test-classifier
DB        := classifier.db
ADDR      := :8080
DATA      := ../data/sample_data.jsonl
LOG_LEVEL ?= info

.PHONY: build run test cover load smoke stress lint fmt clean help

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  build   Compile the server binary"
	@echo "  run     Build and run the server (DB=$(DB), addr=$(ADDR), log-level=$(LOG_LEVEL))"
	@echo "  test    Run the full test suite"
	@echo "  cover   Run tests and show per-function coverage (excludes cmd/)"
	@echo "  load    Stream sample data into a running server"
	@echo "  smoke   Run smoke tests and save server logs to smoke.log"
	@echo "  stress  Run extended stat + LLM escalation scenarios (stress.log)"
	@echo "  lint    Run go vet"
	@echo "  fmt     Format all Go source files with gofmt"
	@echo "  clean   Remove binary and database"

build:
	go build -o $(BIN) ./cmd/server

run: build
	LOG_LEVEL=$(LOG_LEVEL) ./$(BIN) -addr $(ADDR) -db $(DB)

test:
	go test ./...

cover:
	go test ./internal/... -coverprofile=coverage.out
	go tool cover -func=coverage.out
	@rm -f coverage.out

load:
	go run ./cmd/load -addr http://localhost$(ADDR) -file $(DATA)

smoke:
	@bash smoke.sh

stress:
	@bash stress.sh

lint:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

clean:
	rm -f $(BIN) $(DB)
