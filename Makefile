# Docker Compose command (default: docker compose v2)
COMPOSE_CMD ?= podman compose

.PHONY: up down clean restart build logs ps help

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
