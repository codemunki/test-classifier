VENV        := .venv
PYTHON      := $(VENV)/bin/python
PIP         := $(VENV)/bin/pip
SCRIPT      := classify.py
DATA        := ../data/sample_data.jsonl
RESULTS     := results.txt
RESULTS_LLM := results-llm.txt

.PHONY: setup run run-llm run-llm-only lint fmt clean help

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  setup        Create .venv and install dependencies"
	@echo "  run          Run statistical classifier, write $(RESULTS)"
	@echo "  run-llm      Run both classifiers, write $(RESULTS_LLM) (requires ANTHROPIC_API_KEY)"
	@echo "  run-llm-only Run LLM classifier only, write $(RESULTS_LLM) (requires ANTHROPIC_API_KEY)"
	@echo "  lint         Check code with ruff"
	@echo "  fmt          Format and auto-fix code with ruff"
	@echo "  clean        Remove .venv, results files, and cached bytecode"

setup: $(VENV)/bin/activate

$(VENV)/bin/activate:
	python3 -m venv $(VENV)
	$(PIP) install --quiet --upgrade pip
	$(PIP) install --quiet anthropic ruff
	@echo "venv ready — activate with: source $(VENV)/bin/activate"

run: $(VENV)/bin/activate
	$(PYTHON) $(SCRIPT) | tee $(RESULTS)

run-llm: $(VENV)/bin/activate
	@test -n "$$ANTHROPIC_API_KEY" || (echo "ERROR: ANTHROPIC_API_KEY is not set"; exit 1)
	$(PYTHON) $(SCRIPT) --llm | tee $(RESULTS_LLM)

run-llm-only: $(VENV)/bin/activate
	@test -n "$$ANTHROPIC_API_KEY" || (echo "ERROR: ANTHROPIC_API_KEY is not set"; exit 1)
	$(PYTHON) $(SCRIPT) --llm-only | tee $(RESULTS_LLM)

lint: $(VENV)/bin/activate
	$(VENV)/bin/ruff check $(SCRIPT)

fmt: $(VENV)/bin/activate
	$(VENV)/bin/ruff format $(SCRIPT)
	$(VENV)/bin/ruff check --fix $(SCRIPT)

clean:
	rm -rf $(VENV) __pycache__ *.pyc $(RESULTS) $(RESULTS_LLM)
