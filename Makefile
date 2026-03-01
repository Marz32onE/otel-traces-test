# Docker Compose command (default: docker compose v2)
COMPOSE_CMD ?= podman compose

# Kind: assume cluster already exists (kind create cluster + kubectl + helm)
KIND_CLUSTER ?= kind
HELM_RELEASE ?= otel-traces-test
HELM_NAMESPACE ?= otel
# Use docker or podman for building images (e.g. DOCKER_CMD=podman)
DOCKER_CMD ?= docker

.PHONY: up down clean restart build logs ps help
.PHONY: kind-build kind-install kind-uninstall kind-verify kind-up kind-down

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

help:
	@echo "Usage:"
	@echo "  make up      - Start all services (docker compose up -d --build)"
	@echo "  make down    - Stop all services"
	@echo "  make clean   - Stop and remove containers + volumes"
	@echo "  make restart - down then up"
	@echo "  make build   - Build images only"
	@echo "  make ps      - Show service status"
	@echo "  make logs    - Follow logs (optional: make logs SVC=api)"
	@echo "  make help    - This message"
	@echo ""
	@echo "Kind (cluster must exist):"
	@echo "  make kind-build     - Build api/worker/frontend images and load into kind"
	@echo "  make kind-install   - Helm upgrade --install $(HELM_RELEASE) charts/otel-traces-test -n $(HELM_NAMESPACE)"
	@echo "  make kind-uninstall - Helm uninstall $(HELM_RELEASE) -n $(HELM_NAMESPACE)"
	@echo "  make kind-up        - kind-build + kind-install"
	@echo "  make kind-down      - kind-uninstall"
	@echo "  make kind-verify    - Wait for pods and curl API via port-forward"

# --- Kind: build images and load into cluster ---
kind-build:
	$(DOCKER_CMD) build -t localhost/otel-traces-test-api:latest -f api/Dockerfile .
	$(DOCKER_CMD) build -t localhost/otel-traces-test-worker:latest -f worker/Dockerfile .
	$(DOCKER_CMD) build -t localhost/otel-traces-test-frontend:latest \
		--build-arg VITE_API_URL=http://localhost:8081 \
		--build-arg VITE_WS_URL=ws://localhost:8082 \
		--build-arg VITE_OTEL_COLLECTOR_URL=http://localhost:4318 \
		-f frontend/Dockerfile ./frontend
	@# Podman: kind load docker-image does not see podman store; use save + load image-archive
	@$(DOCKER_CMD) save -o /tmp/otel-traces-test-api.tar localhost/otel-traces-test-api:latest
	@$(DOCKER_CMD) save -o /tmp/otel-traces-test-worker.tar localhost/otel-traces-test-worker:latest
	@$(DOCKER_CMD) save -o /tmp/otel-traces-test-frontend.tar localhost/otel-traces-test-frontend:latest
	kind load image-archive /tmp/otel-traces-test-api.tar /tmp/otel-traces-test-worker.tar /tmp/otel-traces-test-frontend.tar --name $(KIND_CLUSTER)
	@rm -f /tmp/otel-traces-test-api.tar /tmp/otel-traces-test-worker.tar /tmp/otel-traces-test-frontend.tar

kind-install:
	helm upgrade --install $(HELM_RELEASE) ./charts/otel-traces-test -n $(HELM_NAMESPACE) --create-namespace

kind-uninstall:
	helm uninstall $(HELM_RELEASE) -n $(HELM_NAMESPACE) 2>/dev/null || true

kind-up: kind-build kind-install

kind-down: kind-uninstall

kind-verify:
	kubectl wait --for=condition=ready pod -l app=nats -n $(HELM_NAMESPACE) --timeout=120s
	kubectl wait --for=condition=ready pod -l app=api -n $(HELM_NAMESPACE) --timeout=120s
	@kubectl port-forward -n $(HELM_NAMESPACE) svc/$(HELM_RELEASE)-api 8081:8081 & PID=$$!; \
	sleep 3; curl -s -X POST http://localhost:8081/api/message -H "Content-Type: application/json" -d '{"text":"kind-verify"}'; \
	kill $$PID 2>/dev/null || true
