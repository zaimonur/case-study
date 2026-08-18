# EatBetter Backend

Phase 1 provides the backend foundation for the EatBetter case study: a Go REST API, PostgreSQL, Docker-based local development, explicit migrations, health checks, structured logging, and graceful shutdown. Food and meal domain behavior is intentionally not part of this phase.

## Prerequisites

- Docker with Docker Compose v2
- Go 1.26 only when running or testing outside Docker

## Quick start

Start PostgreSQL and the API:

```sh
docker compose up
```

Compose supplies safe local defaults, so copying an environment file is optional. To customize the defaults:

```sh
cp .env.example .env
docker compose up --build
```

The API is available at `http://localhost:8080` by default.

## Health endpoints

| Endpoint | Purpose | Healthy response | Failure behavior |
| --- | --- | --- | --- |
| `GET /health` | Process liveness | `200 {"status":"ok"}` | Independent of PostgreSQL |
| `GET /ready` | Traffic readiness | `200 {"status":"ready"}` | `503 {"status":"not_ready"}` when PostgreSQL cannot be reached within the configured timeout |

Every handled request receives an `X-Request-ID` response header. Access logs are JSON and include request ID, method, path, status, and duration; request bodies and connection strings are not logged.

## Configuration

`.env.example` documents all supported settings. The application fails fast when required or invalid configuration is supplied.

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | required outside Compose | PostgreSQL connection URL |
| `APP_ENV` | `development` | Runtime environment label included in logs |
| `HTTP_PORT` | `8080` | API listen port; in Compose this configures the host port |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Maximum time to read request headers |
| `HTTP_IDLE_TIMEOUT` | `60s` | HTTP keep-alive idle timeout |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful HTTP drain deadline |
| `DB_MAX_CONNS` | `10` | Maximum PostgreSQL pool size |
| `DB_MIN_CONNS` | `1` | Minimum PostgreSQL pool size; `0` disables pre-warmed connections |
| `DB_MAX_CONN_LIFETIME` | `30m` | Maximum lifetime of a pooled connection |
| `DB_PING_TIMEOUT` | `2s` | Startup and readiness ping deadline |

`POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, and `POSTGRES_PORT` configure the local Compose database. The example credentials are development-only and must not be reused in a deployed environment.

## Migrations

Schema changes are deliberately separate from API startup:

```sh
make migrate-up
make migrate-down
```

Equivalent Compose commands are:

```sh
docker compose run --rm migrate up
docker compose run --rm migrate down 1
```

The initial no-op migration verifies the migration lifecycle and creates only `golang-migrate`'s version metadata. It does not create food, meal, or other domain tables.

## Local Go workflow

Start PostgreSQL with `docker compose up db`, export the settings from `.env.example`, and then run:

```sh
go run ./cmd/api
```

Common validation commands:

```sh
make test
make vet
make build
# or all three
make verify
```

## Project structure

```text
cmd/api/                    process composition and lifecycle
internal/config/            environment loading and validation
internal/httpapi/           routes, middleware, and HTTP server configuration
internal/platform/database/ PostgreSQL pool construction
migrations/                 versioned schema changes
```

The structure leaves room for feature-oriented domain and application packages in later phases without adding unused repositories, services, factories, or provider abstractions now.

## Technical decisions

- Standard-library `net/http` and `log/slog` keep the foundation small while providing production-relevant server controls and structured JSON logs.
- `pgxpool` owns PostgreSQL connections. Startup verifies connectivity; readiness continues to reflect database availability while liveness remains independent.
- Migrations are an explicit operational step. API replicas therefore cannot race to mutate the schema during startup, and rollbacks remain deliberate.
- The process listens only after configuration and PostgreSQL are valid. On `SIGINT` or `SIGTERM`, HTTP traffic drains before the pool closes.
- Docker Compose waits for PostgreSQL's health check and uses a named volume. PostgreSQL 18's volume is mounted at `/var/lib/postgresql`, matching the image's major-version-aware data layout.

## Phase 1 boundaries

This phase does not include food or meal models, nutrition providers, AI/LLM integration, authentication, caching, queues, WebSockets, or mobile changes.
