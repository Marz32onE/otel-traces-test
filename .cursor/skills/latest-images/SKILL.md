---
name: latest-images
description: Keeps Docker images in docker-compose.yml and Dockerfiles at latest compatible versions. Defines compatibility rules (e.g. Tempo 2.9.0, nats:alpine) and verification steps. Use when editing docker-compose.yml, Dockerfiles, or when the user asks to upgrade or pin image versions.
---

# Latest Docker Images

All Docker images used in this project must be the latest available version that is **compatible** with our configuration. Before considering any task involving Docker services complete, verify image versions.

## Rules

1. **Always use latest** — every image in `docker-compose.yml` and `**/Dockerfile` should use the newest version that works with the project's config files.
2. **Test after upgrade** — pull the new image, start the service, and confirm it runs without errors (check `docker compose ps` and `docker compose logs`).
3. **Pin when latest breaks** — if `latest` or a newer tag fails, find the newest working version and pin it. Add a comment in `docker-compose.yml` explaining why.

## Known Compatibility Constraints

| Image | Constraint | Reason |
|---|---|---|
| `grafana/tempo` | Pin to `2.9.0` (NOT `latest` / `2.10+`) | v2.10+ requires partition ring + memcached; `compactor` field removed from `app.Config`; simple local-storage config incompatible |
| `nats` | Use `nats:alpine` (NOT `nats:latest`) | `nats:latest` is a minimal image without `wget`; healthcheck in `docker-compose.yml` depends on `wget` |
| `grafana/grafana` | `latest` works | No known constraints |
| `otel/opentelemetry-collector-contrib` | `latest` works | No known constraints |

## Verification Steps

When editing `docker-compose.yml` or any `Dockerfile`:

```bash
# 1. Pull all images
docker compose pull

# 2. Rebuild custom images
docker compose build --no-cache

# 3. Start and check
docker compose up -d
docker compose ps --format "table {{.Name}}\t{{.Status}}"

# 4. Confirm no crashes or errors
docker compose logs --tail 5 2>&1 | grep -i "error\|fatal\|panic"
```

## Checking for Newer Versions

```bash
# Check latest release via GitHub API
curl -s "https://api.github.com/repos/<org>/<repo>/releases/latest" | grep tag_name

# Examples:
# Tempo: repos/grafana/tempo/releases/latest
# NATS:  repos/nats-io/nats-server/releases/latest
```

When a newer version is found, update the image tag, run verification, and update the compatibility table above if the new version introduces breaking changes.
