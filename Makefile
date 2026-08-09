SHELL := /bin/sh
COMPOSE := docker compose --env-file .env -f deploy/compose/compose.yaml
MCP_BASE_URL ?= http://127.0.0.1:8080
MCP_URL := $(MCP_BASE_URL)/mcp

.PHONY: help env-init mcp-preflight preflight fmt check test build migrate milvus-init doctor reindex up up-gpu down logs mcp-start mcp-start-gpu mcp-status mcp-logs mcp-stop codex-login codex-check codex codex-repo workpacket-build workpacket-evaluate workpacket-verify diagram-review-loop pull-local-model clean

help:
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "%-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

env-init: ## Create .env with random local secrets; never overwrites an existing file
	@command -v openssl >/dev/null 2>&1 || { echo "openssl is required" >&2; exit 1; }
	@test -f .env.example || { echo "missing .env.example" >&2; exit 1; }
	@test ! -e .env || { echo ".env already exists; refusing to overwrite it" >&2; exit 1; }
	@umask 077; \
	env_tmp=$$(mktemp .env.tmp.XXXXXX); \
	trap 'rm -f "$$env_tmp"' 0 1 2 3 15; \
	auth_token_value=$$(openssl rand -hex 32); \
	postgres_password_value=$$(openssl rand -hex 32); \
	workspace_root=$$(pwd -P); \
	sed \
		-e "s|^AUTH_TOKEN=CHANGE_ME.*|AUTH_TOKEN=$$auth_token_value|" \
		-e "s|^POSTGRES_PASSWORD=CHANGE_ME.*|POSTGRES_PASSWORD=$$postgres_password_value|" \
		-e "s|^CODEGRAPH_HOST_ROOT=.*|CODEGRAPH_HOST_ROOT=$$workspace_root|" \
		.env.example > "$$env_tmp"; \
	chmod 600 "$$env_tmp"; \
	mv "$$env_tmp" .env; \
	echo "created .env with mode 0600; secrets were not printed"

mcp-preflight: ## Validate local tools, .env secrets, and Compose configuration
	@command -v docker >/dev/null 2>&1 || { echo "docker is required" >&2; exit 1; }
	@command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }
	@command -v git >/dev/null 2>&1 || { echo "git is required" >&2; exit 1; }
	@test -f .env || { echo "missing .env; run 'make env-init'" >&2; exit 1; }
	@auth_token_value=$$(sed -n 's/^[[:space:]]*AUTH_TOKEN[[:space:]]*=[[:space:]]*//p' .env | tail -n 1); \
	postgres_password_value=$$(sed -n 's/^[[:space:]]*POSTGRES_PASSWORD[[:space:]]*=[[:space:]]*//p' .env | tail -n 1); \
	case "$$auth_token_value" in ""|CHANGE_ME*) echo "set a real AUTH_TOKEN in .env" >&2; exit 1;; esac; \
	case "$$postgres_password_value" in ""|CHANGE_ME*) echo "set a real POSTGRES_PASSWORD in .env" >&2; exit 1;; esac
	@docker compose version >/dev/null
	@$(COMPOSE) config --quiet
	@echo "MCP preflight passed"

preflight: mcp-preflight codex-check ## Validate both MCP platform and Codex client prerequisites

fmt: ## Format Go sources
	gofmt -w cmd components internal migrations

check: ## Run formatting, vet, and unit tests
	test -z "$$(gofmt -l cmd components internal migrations)"
	go vet ./...
	go test -race ./...

test: ## Run unit tests
	go test ./...

build: ## Build all binaries into ./bin
	mkdir -p bin
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/worker ./cmd/worker
	go build -o bin/admin ./cmd/admin
	go build -o bin/workpacket ./cmd/workpacket

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

mcp-start: mcp-preflight ## Start the local platform and MCP gateway in the background
	$(COMPOSE) up --build -d

mcp-start-gpu: mcp-preflight ## Start the local platform and MCP gateway with NVIDIA GPU support
	$(COMPOSE) -f deploy/compose/compose.gpu.yaml up --build -d

mcp-status: mcp-preflight ## Show containers and verify gateway liveness/readiness
	@$(COMPOSE) ps
	@curl --fail --silent --show-error --max-time 5 "$(MCP_BASE_URL)/healthz"; echo
	@curl --fail --silent --show-error --max-time 5 "$(MCP_BASE_URL)/readyz"; echo

mcp-logs: mcp-preflight ## Follow the separately running MCP gateway and worker
	$(COMPOSE) logs -f gateway worker

mcp-stop: mcp-preflight ## Stop the MCP platform while retaining all named volumes
	$(COMPOSE) down

codex-login: ## Authenticate Codex through the interactive ChatGPT browser flow
	@command -v codex >/dev/null 2>&1 || { echo "codex is required" >&2; exit 1; }
	codex login

codex-check: mcp-preflight ## Check Codex authentication and MCP registration
	@command -v codex >/dev/null 2>&1 || { echo "codex is required" >&2; exit 1; }
	@codex login status
	@codex mcp list

codex: mcp-preflight ## Start Codex in this repository with the MCP bearer token loaded
	@command -v codex >/dev/null 2>&1 || { echo "codex is required" >&2; exit 1; }
	@codex login status >/dev/null || { echo "Codex is signed out; run 'make codex-login'" >&2; exit 1; }
	@curl --fail --silent --show-error --max-time 5 "$(MCP_BASE_URL)/healthz" >/dev/null || { echo "MCP gateway is unavailable; start Terminal 1 with 'make mcp-start' or 'make mcp-start-gpu'" >&2; exit 1; }
	@auth_token_value=$$(sed -n 's/^[[:space:]]*AUTH_TOKEN[[:space:]]*=[[:space:]]*//p' .env | tail -n 1); \
	case "$$auth_token_value" in ""|CHANGE_ME*) echo "set a real AUTH_TOKEN in .env" >&2; exit 1;; esac; \
	HYBRID_AI_MCP_TOKEN="$$auth_token_value" exec codex

codex-repo: mcp-preflight ## Start Codex for REPO=/absolute/path with this HTTP MCP server
	@command -v codex >/dev/null 2>&1 || { echo "codex is required" >&2; exit 1; }
	@test -n "$(REPO)" || { echo "usage: make codex-repo REPO=/absolute/path/to/repository" >&2; exit 1; }
	@codex login status >/dev/null || { echo "Codex is signed out; run 'make codex-login'" >&2; exit 1; }
	@curl --fail --silent --show-error --max-time 5 "$(MCP_BASE_URL)/healthz" >/dev/null || { echo "MCP gateway is unavailable; start Terminal 1 first" >&2; exit 1; }
	@auth_token_value=$$(sed -n 's/^[[:space:]]*AUTH_TOKEN[[:space:]]*=[[:space:]]*//p' .env | tail -n 1); \
	repository_path=$$(cd "$(REPO)" 2>/dev/null && pwd -P) || { echo "REPO is not an accessible directory: $(REPO)" >&2; exit 1; }; \
	HYBRID_AI_MCP_TOKEN="$$auth_token_value" exec codex -C "$$repository_path" \
		-c 'mcp_servers.hybrid_knowledge.url="$(MCP_URL)"' \
		-c 'mcp_servers.hybrid_knowledge.bearer_token_env_var="HYBRID_AI_MCP_TOKEN"' \
		-c 'mcp_servers.hybrid_knowledge.required=true' \
		-c 'mcp_servers.hybrid_knowledge.startup_timeout_sec=20' \
		-c 'mcp_servers.hybrid_knowledge.tool_timeout_sec=120' \
		-c 'mcp_servers.hybrid_knowledge.default_tools_approval_mode="writes"' \
		-c 'mcp_servers.hybrid_knowledge.tools.knowledge_candidate_decide.approval_mode="prompt"' \
		-c 'mcp_servers.hybrid_knowledge.tools.repository_relation_upsert.approval_mode="prompt"' \
		-c 'mcp_servers.hybrid_knowledge.tools.code_repository_index.approval_mode="prompt"'

workpacket-build: ## Build the independent bounded-work policy verifier
	mkdir -p bin
	go build -o bin/workpacket ./cmd/workpacket

workpacket-evaluate: workpacket-build ## Evaluate PACKET=/path/to/work-packet.json without executing it
	@test -n "$(PACKET)" || { echo "usage: make workpacket-evaluate PACKET=/path/to/work-packet.json" >&2; exit 1; }
	./bin/workpacket evaluate --packet "$(PACKET)"

workpacket-verify: workpacket-build ## Verify PATCH=/path/to/change.patch in an isolated clone using PACKET
	@test -n "$(PACKET)" || { echo "PACKET is required" >&2; exit 1; }
	@test -n "$(PATCH)" || { echo "usage: make workpacket-verify PACKET=/path/to/work-packet.json PATCH=/path/to/change.patch" >&2; exit 1; }
	./bin/workpacket verify --packet "$(PACKET)" --patch "$(PATCH)"

diagram-review-loop: ## Render the review-learning Mermaid diagram as SVG and high-resolution PNG
	npx -y @mermaid-js/mermaid-cli@11.16.0 -p docs/diagrams/puppeteer-config.json -i docs/diagrams/hybrid-ai-review-learning-loop.mmd -o docs/diagrams/hybrid-ai-review-learning-loop.svg -b '#ffffff'
	npx -y @mermaid-js/mermaid-cli@11.16.0 -p docs/diagrams/puppeteer-config.json -i docs/diagrams/hybrid-ai-review-learning-loop.mmd -o docs/diagrams/hybrid-ai-review-learning-loop.png -b '#ffffff' -w 6400 -s 2

pull-local-model: ## Pull the recommended GBX100 coding model
	docker compose --env-file .env -f deploy/compose/compose.yaml exec ollama ollama pull "$${LOCAL_CHAT_MODEL:-qwen3.6:35b}"

clean: ## Remove build/test output only; persistent service data is retained
	rm -rf bin coverage.out
