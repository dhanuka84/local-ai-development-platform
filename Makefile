SHELL := /bin/sh
COMPOSE := docker compose --env-file .env -f deploy/compose/compose.yaml
MCP_BASE_URL ?= http://127.0.0.1:8080
MCP_URL := $(MCP_BASE_URL)/mcp
PROJECT ?=
LIMIT ?= 25
ID ?=
ACTOR ?=

.PHONY: help help-operations help-development help-qa help-product-owner env-init mcp-preflight preflight fmt check test build migrate milvus-init doctor reindex candidate-list candidate-get candidate-approve candidate-reject up up-gpu down logs mcp-start mcp-start-gpu mcp-status mcp-logs mcp-stop codex-login codex-check codex codex-repo workpacket-build workpacket-evaluate workpacket-verify diagram-review-loop pull-local-model clean ops-start ops-start-gpu ops-status ops-logs ops-stop ops-doctor ops-reindex dev-session dev-session-repo dev-policy-check dev-patch-verify dev-check qa-session qa-session-repo qa-patch-verify qa-check qa-candidates qa-candidate-get po-candidates po-candidate-get po-approve po-reject

help: ## Show all commands plus role-specific workflow guides
	@printf '%s\n' \
		'Role workflows:' \
		'  make help-operations' \
		'  make help-development' \
		'  make help-qa' \
		'  make help-product-owner' \
		'' \
		'All commands:'
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "%-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

help-operations: ## Show the Operations workflow and commands
	@printf '%s\n' \
		'OPERATIONS' \
		'One-time setup:' \
		'  1. make env-init' \
		'  2. Review .env and CODEGRAPH_HOST_ROOT' \
		'  3. make mcp-preflight' \
		'  4. make ops-start-gpu       # or: make ops-start' \
		'  5. make pull-local-model' \
		'' \
		'Each platform session:' \
		'  1. make ops-start-gpu       # or: make ops-start' \
		'  2. make ops-status' \
		'  3. make ops-logs             # optional, blocking' \
		'  4. make ops-stop' \
		'' \
		'Administration:' \
		'  make migrate | make milvus-init | make ops-doctor | make ops-reindex'

help-development: ## Show the Development workflow and commands
	@printf '%s\n' \
		'DEVELOPMENT' \
		'One-time setup:' \
		'  1. make codex-login' \
		'  2. make preflight' \
		'' \
		'Each development task:' \
		'  1. make dev-session' \
		'     or: make dev-session-repo REPO=/absolute/path' \
		'  2. Search approved knowledge and repository/code graphs through MCP' \
		'  3. make dev-policy-check PACKET=/path/work-packet.json' \
		'  4. Implement locally or through Codex' \
		'  5. make dev-patch-verify PACKET=... PATCH=...' \
		'  6. make dev-check' \
		'  7. Use generation_capture; hand candidate ID to QA'

help-qa: ## Show the QA workflow and commands
	@printf '%s\n' \
		'QA' \
		'Each validation task:' \
		'  1. make qa-candidates PROJECT=<project> LIMIT=25' \
		'  2. make qa-candidate-get ID=<candidate-uuid>' \
		'  3. make qa-session-repo REPO=/absolute/path' \
		'  4. make qa-patch-verify PACKET=... PATCH=...' \
		'  5. make qa-check' \
		'  6. Record independent findings with review_record through MCP' \
		'  7. Hand technically validated candidate ID to Product Owner' \
		'' \
		'Note: role separation is procedural locally; MCP RBAC is enterprise work.'

help-product-owner: ## Show the Product Owner workflow and commands
	@printf '%s\n' \
		'PRODUCT OWNER' \
		'Each knowledge decision:' \
		'  1. make po-candidates PROJECT=<project> LIMIT=25' \
		'  2. make po-candidate-get ID=<candidate-uuid>' \
		'  3. Confirm QA evidence, applicability, and business acceptance' \
		'  4. make po-approve ID=<candidate-uuid> ACTOR=<identity>' \
		'     or: make po-reject ID=<candidate-uuid> ACTOR=<identity>' \
		'  5. Operations monitors outbox/index completion'

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

migrate: mcp-preflight ## Apply PostgreSQL migrations through the Compose admin image
	$(COMPOSE) run --rm migrate migrate

milvus-init: mcp-preflight ## Create the Milvus collection through the Compose admin image
	$(COMPOSE) run --rm milvus-init milvus-init

doctor: mcp-preflight ## Test PostgreSQL, Ollama, and Milvus from the Compose network
	$(COMPOSE) run --rm migrate doctor

reindex: mcp-preflight ## Requeue approved knowledge for Milvus indexing
	$(COMPOSE) run --rm migrate reindex

candidate-list: mcp-preflight ## List pending candidates; optional PROJECT and LIMIT=25
	@case "$(LIMIT)" in ''|*[!0-9]*) echo "LIMIT must be an integer from 1 to 100" >&2; exit 1;; esac
	@test "$(LIMIT)" -ge 1 -a "$(LIMIT)" -le 100 || { echo "LIMIT must be from 1 to 100" >&2; exit 1; }
	$(COMPOSE) run --rm migrate candidates "$(PROJECT)" "$(LIMIT)"

candidate-get: mcp-preflight ## Fetch one candidate including pending content; requires ID
	@test -n "$(ID)" || { echo "usage: make candidate-get ID=<candidate-uuid>" >&2; exit 1; }
	$(COMPOSE) run --rm migrate get "$(ID)"

candidate-approve: mcp-preflight ## Approve a QA-validated candidate; requires ID and ACTOR
	@test -n "$(ID)" || { echo "ID is required" >&2; exit 1; }
	@test -n "$(ACTOR)" || { echo "usage: make candidate-approve ID=<uuid> ACTOR=<accountable-identity>" >&2; exit 1; }
	$(COMPOSE) run --rm migrate approve "$(ID)" "$(ACTOR)"

candidate-reject: mcp-preflight ## Reject a candidate; requires ID and ACTOR
	@test -n "$(ID)" || { echo "ID is required" >&2; exit 1; }
	@test -n "$(ACTOR)" || { echo "usage: make candidate-reject ID=<uuid> ACTOR=<accountable-identity>" >&2; exit 1; }
	$(COMPOSE) run --rm migrate reject "$(ID)" "$(ACTOR)"

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

# Operations aliases. These expose role intent while retaining one canonical
# implementation target for each action.
ops-start: mcp-start ## [Operations] Start the CPU platform
ops-start-gpu: mcp-start-gpu ## [Operations] Start the NVIDIA GPU platform
ops-status: mcp-status ## [Operations] Check platform health and readiness
ops-logs: mcp-logs ## [Operations] Follow gateway and worker logs
ops-stop: mcp-stop ## [Operations] Stop the platform and retain data
ops-doctor: doctor ## [Operations] Test PostgreSQL, Ollama, and Milvus
ops-reindex: reindex ## [Operations] Requeue approved semantic projections

# Development aliases.
dev-session: codex ## [Development] Start Codex with the local MCP server
dev-session-repo: codex-repo ## [Development] Start Codex in REPO with local MCP
dev-policy-check: workpacket-evaluate ## [Development] Evaluate PACKET policy
dev-patch-verify: workpacket-verify ## [Development] Verify PATCH using PACKET
dev-check: check ## [Development] Run formatting, vet, and race tests

# QA aliases. QA records its decision with review_record through MCP; these
# commands do not perform final knowledge approval.
qa-session: codex ## [QA] Start an MCP-connected Codex validation session
qa-session-repo: codex-repo ## [QA] Start validation in REPO
qa-patch-verify: workpacket-verify ## [QA] Independently verify PATCH using PACKET
qa-check: check ## [QA] Run the repository verification suite
qa-candidates: candidate-list ## [QA] List pending candidates
qa-candidate-get: candidate-get ## [QA] Read one pending candidate

# Product Owner aliases. Final decisions remain explicit and require the
# accountable actor identity on every invocation.
po-candidates: candidate-list ## [Product Owner] List pending candidates
po-candidate-get: candidate-get ## [Product Owner] Read one pending candidate
po-approve: candidate-approve ## [Product Owner] Approve ID as ACTOR
po-reject: candidate-reject ## [Product Owner] Reject ID as ACTOR
