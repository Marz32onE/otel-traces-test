# --- Auto-detected (override with make VAR=value) ---

# Compose: docker compose | podman-compose | podman compose
COMPOSE_CMD ?= $(shell \
	if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then \
		echo "docker compose"; \
	elif command -v podman-compose >/dev/null 2>&1; then \
		echo "podman-compose"; \
	elif command -v podman >/dev/null 2>&1 && podman compose version >/dev/null 2>&1; then \
		echo "podman compose"; \
	else \
		echo "docker compose"; \
	fi)

# Image builder for kind-build: derive from COMPOSE_CMD (docker or podman)
DOCKER_CMD ?= $(shell \
	first=$$(echo $(COMPOSE_CMD) | cut -d' ' -f1); \
	if [ "$$first" = "podman-compose" ]; then echo podman; else echo $$first; fi)

# Kind cluster name: use single existing cluster, else "kind"
KIND_CLUSTER ?= $(shell \
	clusters=$$(kind get clusters 2>/dev/null); \
	if [ "$$(echo $$clusters | wc -w)" = "1" ] && [ -n "$$clusters" ]; then echo $$clusters; else echo "kind"; fi)

# Helm (project-specific, rarely overridden)
HELM_RELEASE ?= otel-traces-test
HELM_NAMESPACE ?= otel

.PHONY: up down clean restart build logs ps help detect verify-trace up-verify up-ws-trace down-ws-trace logs-ws-trace verify-ws-trace
.PHONY: kind-build kind-install kind-uninstall kind-verify kind-up kind-down

# Show all auto-detected variables
detect:
	@echo "COMPOSE_CMD  = $(COMPOSE_CMD)"
	@echo "DOCKER_CMD   = $(DOCKER_CMD)"
	@echo "KIND_CLUSTER = $(KIND_CLUSTER)"
	@echo "HELM_RELEASE = $(HELM_RELEASE)"
	@echo "HELM_NAMESPACE = $(HELM_NAMESPACE)"

# Start all services in background (build images if needed)
up:
	$(COMPOSE_CMD) up -d --build

# Stop all services
down:
	$(COMPOSE_CMD) down

# Stop and remove all containers, networks, and volumes (full clean)
clean:
	$(COMPOSE_CMD) down -v

# Restart: down then up
restart: down up

# Build images only (no start)
build:
	$(COMPOSE_CMD) build

# Show service status
ps:
	$(COMPOSE_CMD) ps

# Tail logs (all services; use e.g. make logs SVC=api for one)
logs:
	$(COMPOSE_CMD) logs -f $(SVC)

# Check whole trace propagation path (stack must be up: make up first)
verify-trace:
	@bash scripts/check-trace-propagation.sh

# Bring up stack then run trace propagation check
up-verify: up
	@echo "Waiting 25s for services to be ready..."
	@sleep 25
	@bash scripts/check-trace-propagation.sh

help:
	@echo "Usage:"
	@echo "  make up     - Start all services ($(COMPOSE_CMD) up -d --build)"
	@echo "  make down   - Stop all services"
	@echo "  make clean  - Stop and remove containers + volumes"
	@echo "  make restart - down then up"
	@echo "  make build  - Build images only"
	@echo "  make ps     - Show service status"
	@echo "  make logs         - Follow logs (optional: make logs SVC=api)"
	@echo "  make verify-trace  - Check trace propagation (API→Tempo); run after 'make up'"
	@echo "  make up-verify    - make up, wait, then verify-trace"
	@echo "  make up-ws-trace   - Start minimal WS trace stack ($(COMPOSE_CMD) -f docker-compose.ws-trace.yml up -d --build)"
	@echo "  make down-ws-trace - Stop minimal WS trace stack"
	@echo "  make logs-ws-trace - Follow logs (optional: make logs-ws-trace SVC=ws-node-backend)"
	@echo "  make verify-ws-trace - Verify websocket trace propagation (Tempo)"
	@echo "  make detect       - Show auto-detected COMPOSE_CMD, DOCKER_CMD, KIND_CLUSTER"
	@echo "  make help         - This message"
	@echo ""
	@echo "Kind (cluster must exist, KIND_CLUSTER=$(KIND_CLUSTER)):"
	@echo "  make kind-build     - Build images with $(DOCKER_CMD) and load into kind"
	@echo "  make kind-install   - Helm upgrade --install $(HELM_RELEASE) ./charts/otel-traces-test -n $(HELM_NAMESPACE)"
	@echo "  make kind-uninstall - Helm uninstall $(HELM_RELEASE) -n $(HELM_NAMESPACE)"
	@echo "  make kind-up        - kind-build + kind-install"
	@echo "  make kind-down      - kind-uninstall"
	@echo "  make kind-verify    - Wait for pods and curl API via port-forward"

# --- Kind: build images and load into cluster ---
kind-build:
	$(DOCKER_CMD) build -t localhost/otel-traces-test-api:latest -f api/Dockerfile .
	$(DOCKER_CMD) build -t localhost/otel-traces-test-worker:latest -f worker/Dockerfile .
	$(DOCKER_CMD) build -t localhost/otel-traces-test-frontend:latest \
		--build-arg VITE_API_URL=http://localhost:8088 \
		--build-arg VITE_WS_URL=ws://localhost:8082 \
		--build-arg VITE_OTEL_COLLECTOR_URL=http://localhost:4318 \
		-f frontend/Dockerfile ./frontend
	@# Podman: kind load docker-image does not see podman store; use save + load image-archive
	@$(DOCKER_CMD) save -o /tmp/otel-traces-test-api.tar localhost/otel-traces-test-api:latest
	@$(DOCKER_CMD) save -o /tmp/otel-traces-test-worker.tar localhost/otel-traces-test-worker:latest
	@$(DOCKER_CMD) save -o /tmp/otel-traces-test-frontend.tar localhost/otel-traces-test-frontend:latest
	kind load image-archive /tmp/otel-traces-test-api.tar /tmp/otel-traces-test-worker.tar /tmp/otel-traces-test-frontend.tar --name $(KIND_CLUSTER)
	@rm -f /tmp/otel-traces-test-api.tar /tmp/otel-traces-test-worker.tar /tmp/otel-traces-test-frontend.tar

# --- Minimal: frontend+Node WS trace ---
WS_TRACE_COMPOSE_FILE ?= docker-compose.ws-trace.yml

up-ws-trace:
	@set -e; \
	REPO_ROOT="$$PWD"; \
	if [ -z "$$DOCKER_CONFIG" ]; then mkdir -p "$$REPO_ROOT/.docker-config"; export DOCKER_CONFIG="$$REPO_ROOT/.docker-config"; fi; \
	cd "$$REPO_ROOT/pkg/instrumentation-js"; \
	[ -d node_modules ] || npm install; \
	npm run build --workspaces; \
	cd "$$REPO_ROOT"; \
	$(COMPOSE_CMD) -f $(WS_TRACE_COMPOSE_FILE) up -d --build

down-ws-trace:
	$(COMPOSE_CMD) -f $(WS_TRACE_COMPOSE_FILE) down -v

logs-ws-trace:
	$(COMPOSE_CMD) -f $(WS_TRACE_COMPOSE_FILE) logs -f $(SVC)

verify-ws-trace:
	@bash scripts/verify-ws-trace.sh

kind-install:
	helm upgrade --install $(HELM_RELEASE) ./charts/otel-traces-test -n $(HELM_NAMESPACE) --create-namespace

kind-uninstall:
	helm uninstall $(HELM_RELEASE) -n $(HELM_NAMESPACE) 2>/dev/null || true

kind-up: kind-build kind-install

kind-down: kind-uninstall

kind-verify:
	kubectl wait --for=condition=ready pod -l app=nats -n $(HELM_NAMESPACE) --timeout=120s
	kubectl wait --for=condition=ready pod -l app=api -n $(HELM_NAMESPACE) --timeout=120s
	@kubectl port-forward -n $(HELM_NAMESPACE) svc/$(HELM_RELEASE)-api 8088:8088 & PID=$$!; \
	sleep 3; curl -s -X POST http://localhost:8088/api/message -H "Content-Type: application/json" -d '{"text":"kind-verify"}'; \
	kill $$PID 2>/dev/null || true
