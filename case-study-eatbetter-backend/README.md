# EatBetter Backend

The EatBetter backend includes the Phase 1 runtime foundation, the Phase 2 canonical food domain, and the Phase 3B USDA FoodData Central bulk importer. It provides a Go REST API, PostgreSQL, Docker-based local development, explicit migrations, health checks, structured logging, graceful shutdown, pure-Go food models, and a bounded-memory import path. Search, meals, nutrition calculations, USDA HTTP API access, and mobile changes are intentionally not implemented yet.

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

The migrations are:

- `000001_foundation`: verifies the migration lifecycle without creating domain objects.
- `000002_food_domain`: creates canonical foods, per-100-gram nutrition, household portions, multilingual aliases, and external source references.
- `000003_food_identifiers`: adds provider-neutral stable product identifiers, initially the `gtin_upc` scheme.

The food migration is reversible. Removing one migration version drops only the Phase 2 domain tables and leaves the foundation migration applied.

## Canonical food domain

The pure-Go models live in `internal/domain/food` and do not depend on PostgreSQL, HTTP models, or external provider DTOs.

- A `Food` has an internal identity, canonical name, and optional brand.
- `Nutrition` uses one canonical basis: calories in kcal/100 g and macros in g/100 g.
- An unavailable nutrient is distinct from a known zero in both Go and PostgreSQL.
- A `Portion` maps an amount and free-form household measure such as `slice` or `cup, chopped` to grams.
- A `FoodAlias` may carry a nullable language tag; language-aware resolution remains future work.
- An `ExternalFoodReference` associates a canonical food with a string identifier owned by a `FoodSource`, currently `usda` or `open_food_facts`.
- A `FoodIdentifier` associates stable retail identity with a food. GTIN/UPC values remain text, including leading zeroes.

Persisted nutrition and portion amounts use `NUMERIC(12,4)`. Internal IDs use PostgreSQL `BIGINT GENERATED ALWAYS AS IDENTITY`. Deleting a canonical food cascades to its nutrition, portions, aliases, and external references.

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

## USDA FoodData Central bulk import

Apply all migrations, then point the standalone importer at an extracted USDA full CSV download:

```sh
DATABASE_URL='postgres://eatbetter:eatbetter@localhost:5432/eatbetter?sslmode=disable' \
  go run ./cmd/usda-import \
  --dataset-dir "$HOME/Desktop/FoodData_Central_csv_2026-04-30" \
  --dataset-date 2026-04-30
```

The importer validates exact required CSV headers and canonical nutrient dictionary tuples before opening its write transaction. It streams source CSV rows, keeps only candidate identity/nutrient state in memory, writes canonical-shaped temporary tables with PostgreSQL `COPY`, and atomically merges them under an advisory lock. It does not create a persistent raw USDA mirror.

For branded foods, exact source GTIN/UPC text is the stable canonical identity. The selected latest USDA version updates the canonical payload while every observed USDA FDC ID is retained as a historical `external_food_refs` relationship under the same food. Generic Foundation, FNDDS, and SR Legacy foods use FDC ID identity. Missing nutrients remain `NULL`, while a source value of zero remains a known zero.

FNDDS portion descriptions are retained verbatim as source-native free-form measure phrases with `Amount=1`; they are deliberately not parsed. Branded servings become portions only when USDA supplies a positive serving size with the literal `g` unit. No density or `ml`-to-gram conversion is attempted.

Provider CSV DTOs live in `internal/adapters/usda/bulkcsv`, mapping/orchestration contracts live in `internal/application/foodimport`, and canonical domain types remain provider-neutral. A future USDA HTTP adapter can therefore produce the same application import records without coupling the domain to CSV layout.

## Project structure

```text
cmd/api/                    process composition and lifecycle
cmd/usda-import/            USDA bulk import executable
internal/adapters/usda/     provider-specific streaming CSV adapter
internal/application/       provider-neutral import orchestration contracts
internal/config/            environment loading and validation
internal/domain/food/       canonical food domain vocabulary and invariants
internal/httpapi/           routes, middleware, and HTTP server configuration
internal/platform/database/ PostgreSQL pool and transactional import persistence
migrations/                 versioned schema changes
```

The structure leaves room for feature-oriented domain and application packages in later phases without adding unused repositories, services, factories, or provider abstractions now.

## Technical decisions

- Standard-library `net/http` and `log/slog` keep the foundation small while providing production-relevant server controls and structured JSON logs.
- `pgxpool` owns PostgreSQL connections. Startup verifies connectivity; readiness continues to reflect database availability while liveness remains independent.
- Migrations are an explicit operational step. API replicas therefore cannot race to mutate the schema during startup, and rollbacks remain deliberate.
- The food domain uses fixed MVP nutrition fields rather than generic nutrient rows. This keeps missing-versus-zero semantics explicit without introducing a nutrient catalog before it is needed.
- External source identifiers are separate from canonical food IDs, so future USDA and Open Food Facts adapters can map their DTOs into the same domain.
- The process listens only after configuration and PostgreSQL are valid. On `SIGINT` or `SIGTERM`, HTTP traffic drains before the pool closes.
- Docker Compose waits for PostgreSQL's health check and uses a named volume. PostgreSQL 18's volume is mounted at `/var/lib/postgresql`, matching the image's major-version-aware data layout.

## Current boundaries

The current implementation does not include repository CRUD, USDA HTTP API or Open Food Facts adapters, food search or ranking, portion/nutrition calculation engines, meals, AI/LLM integration, authentication, caching, queues, WebSockets, or mobile changes.
