SHELL := /bin/sh
COMPOSE := docker compose --env-file .env -f deploy/compose/compose.yaml

.PHONY: help fmt check test build migrate milvus-init doctor reindex up up-gpu down logs pull-local-model clean

help:
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "%-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## Format Go sources
	gofmt -w cmd internal migrations

check: ## Run formatting, vet, and unit tests
	test -z "$$(gofmt -l cmd internal migrations)"
	go vet ./...
	go test -race ./...

test: ## Run unit tests
	go test ./...

build: ## Build all binaries into ./bin
	mkdir -p bin
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/worker ./cmd/worker
	go build -o bin/admin ./cmd/admin

migrate: ## Apply PostgreSQL migrations using local Go
	go run ./cmd/admin migrate

milvus-init: ## Create the Milvus collection using local Go
	go run ./cmd/admin milvus-init

doctor: ## Test all configured dependencies using local Go
	go run ./cmd/admin doctor

reindex: ## Requeue approved knowledge for Milvus indexing
	go run ./cmd/admin reindex

up: ## Start the local CPU stack
	$(COMPOSE) up --build -d

up-gpu: ## Start the local NVIDIA GPU stack
	$(COMPOSE) -f deploy/compose/compose.gpu.yaml up --build -d

down: ## Stop containers while retaining named volumes
	$(COMPOSE) down

logs: ## Follow gateway and worker logs
	$(COMPOSE) logs -f gateway worker

pull-local-model: ## Pull the recommended GBX100 coding model
	docker compose --env-file .env -f deploy/compose/compose.yaml exec ollama ollama pull "$${LOCAL_CHAT_MODEL:-qwen3.6:35b}"

clean: ## Remove build/test output only; persistent service data is retained
	rm -rf bin coverage.out
