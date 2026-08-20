# EatBetter Backend

The EatBetter backend includes the runtime foundation, canonical food domain, USDA FoodData Central bulk importer, deterministic Turkish localization, multilingual product-aware food search, canonical food detail, trusted portion resolution, and deterministic nutrition calculation. It provides a Go REST API, PostgreSQL, Docker-based local development, explicit migrations, health checks, structured logging, graceful shutdown, pure-Go food models, and a bounded-memory import path. Search returns candidates rather than automatically selecting a food; the backend alone converts a canonical food plus grams into nutrition truth.

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

## Food search

`GET /foods/search?q=yumurta&locale=tr-TR&limit=10` returns an ordered list of canonical food candidates. `q` must contain 2–120 normalized Unicode characters. `limit` defaults to 10 and accepts 1–20. Locale is optional; `tr-TR` searches `tr-TR`, then `tr`, while any valid unsupported locale (for example `de-DE`) still searches and displays canonical data. Malformed input returns `400`, unsupported methods return `405` with `Allow: GET`, and a valid miss returns `200 {"items":[]}`.

The primary normalized form uses NFC, Turkish-aware casing, collapsed whitespace, and safe punctuation separators while retaining `ç`, `ğ`, `ı`, `ö`, `ş`, and `ü`. A second folded form maps these to ASCII for tolerant input such as `sut` and `cig`; a primary-form match outranks an otherwise equivalent folded match.

Retrieval is staged and SQL-bounded: full-string exact first, whole-word/token-sequence matches next when the exact pool lacks enough results or credible generic evidence, prefix under the same rule, then trigram fuzzy matching only when the requested set is still incomplete and the query has at least three characters. If a filled strong stage hides generic evidence, one indexed generic-strong query collects bounded exact/whole-word/prefix evidence while excluding GTIN products. Every executed retrieval has a cap of `max(40, public_limit × 5)`, so product composition may inspect more internal candidates than the public limit without materializing the catalog. Ranking is lexicographic and deterministic: exact before whole-word before prefix before fuzzy, primary before folded, localized display before canonical name before localization alias before food alias before brand, leading whole-word matches before interior whole-word matches, fuzzy similarity within the fuzzy tier, and canonical food ID as the final tie-breaker. Signals from multiple surfaces collapse to one `foods.id`; aliases never become identity.

For ordinary food intent, a maximum of half the response is first filled with the strongest credible generic/common exact, whole-word, or prefix candidates; remaining slots retain normal lexical order. Branded identity is derived from the existing stable `gtin_upc` identifier path, not from nullable brand display metadata. Generic fuzzy noise receives no product-priority lane, so it cannot jump ahead of a strong branded exact result merely by being generic.

Brand intent is resolved only from persisted `foods.brand` evidence. All contiguous phrases from the already length-limited query are checked in one bounded database call, longest credible phrase first; there is no static brand dictionary. Thus both `Kroger milk` and `milk Kroger` resolve `Kroger` as the brand and search the remaining `milk` phrase inside matching branded foods. A complete persisted brand such as `meijer` produces bounded brand-only discovery. If a whole query is both a real brand and a credible generic food term, credible generic food evidence keeps ordinary intent, preventing brand collisions from hiding common foods.

Turkish display names and localization aliases participate only when their locale is relevant and `food_localizations.source_canonical_name = foods.canonical_name`. A stale row therefore cannot retrieve a food or become its display name; the response falls back to `foods.canonical_name` without mutating localization data.

## Food detail

`GET /foods/{id}?locale=tr-TR` returns the canonical identity, fresh localized display fallback, optional brand, canonical per-100-gram nutrition, and trusted stored portions. IDs must be positive integers. A malformed ID or locale returns `400`, a missing food returns `404`, and unsupported methods return `405` with `Allow: GET`. Valid unsupported locales use canonical display data.

```sh
curl 'http://localhost:8080/foods/123?locale=tr-TR'
```

```json
{
  "food_id": 123,
  "display_name": "Tam yağlı süt",
  "canonical_name": "Milk, whole",
  "brand": null,
  "nutrition_per_100g": {
    "calories_kcal": 61,
    "protein_g": 3.15,
    "carbohydrates_g": 4.8,
    "fat_g": null
  },
  "portions": [
    {"portion_id": 456, "amount": 0.5, "measure": "cup", "grams": 120}
  ]
}
```

Unavailable nutrients are JSON `null`; a persisted known zero is numeric `0`. Foods without stored portions return `"portions": []`. Portions are ordered deterministically by amount, measure, grams, then ID.

## Deterministic nutrition calculation

`POST /nutrition/calculate` supports exactly one of two modes. The direct-grams mode needs no portion lookup:

```sh
curl -X POST 'http://localhost:8080/nutrition/calculate' \
  -H 'Content-Type: application/json' \
  -d '{"food_id":123,"grams":56}'
```

The stored-portion mode multiplies one exact persisted portion record:

```sh
curl -X POST 'http://localhost:8080/nutrition/calculate' \
  -H 'Content-Type: application/json' \
  -d '{"food_id":123,"portion_id":456,"quantity":2}'
```

```json
{
  "food_id": 123,
  "resolved_grams": 240,
  "nutrition": {
    "calories_kcal": 146.4,
    "protein_g": 7.56,
    "carbohydrates_g": 11.52,
    "fat_g": null
  }
}
```

`quantity` means “number of instances of the selected stored record,” not a newly interpreted household amount. For a stored record `{amount: 0.5, measure: "cup", grams: 120}`, `quantity: 2` resolves to `240 g`; it does not mean “2 cups.” Free-form measure text is never parsed. Missing or mismatched portions fail and never fall back to an estimate. There is no density inference, free-text unit conversion, or ml-to-gram conversion.

The source of truth is the persisted `food_nutrition` per-100-gram row. Resolved grams are rounded first with Go's deterministic `math.Round(value × 100) / 100` policy, then each known nutrient is scaled from that resolved value and rounded with the same policy. Stored source values are not changed, calories are not derived from macros, unknown values remain `null`, and known zero remains `0`. The request body is limited to 4 KiB; unknown fields, a trailing second JSON value, invalid numbers, and ambiguous/both/neither modes return stable `400 {"status":"invalid_request"}` responses.

AI is intentionally absent from this layer. A future AI resolver must select a canonical `food_id` and then consume this same calculation path; it may not invent nutrition. The React Native client likewise renders the returned result and performs no calorie or macro calculation.

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
- `000004_food_localizations`: adds locale-specific display names and their search-only aliases without changing canonical food data.
- `000005_food_search`: adds immutable primary/folded normalization helpers plus measured B-tree exact and GIN `pg_trgm` prefix/fuzzy indexes over existing search surfaces.

The food migration is reversible. Removing one migration version drops only the Phase 2 domain tables and leaves the foundation migration applied.

## Canonical food domain

The pure-Go models live in `internal/domain/food` and do not depend on PostgreSQL, HTTP models, or external provider DTOs.

- A `Food` has an internal identity, canonical name, and optional brand.
- `Nutrition` uses one canonical basis: calories in kcal/100 g and macros in g/100 g.
- An unavailable nutrient is distinct from a known zero in both Go and PostgreSQL.
- A `Portion` maps an amount and free-form household measure such as `slice` or `cup, chopped` to grams.
- A `FoodAlias` may carry a nullable language tag; search considers neutral aliases plus exact and base-locale aliases for the request.
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

## Deterministic Turkish localization

Phase 4.5 produces conservative Turkish display names for imported generic USDA foods. It does not translate branded foods, call an LLM or translation service, or change USDA canonical names, nutrition, portions, identifiers, or references. A record is localized only when the repository-controlled glossary and rules consume its complete source description; ambiguous and partially matched records remain `review_required` or `untranslated` in the artifact.

Generate the April 2026 JSONL artifact and coverage manifest:

```sh
go run ./cmd/usda-localize generate \
  --dataset-dir "$HOME/Desktop/FoodData_Central_csv_2026-04-30" \
  --dataset-date 2026-04-30 \
  --output data/localizations/usda/2026-04-30/tr.jsonl \
  --manifest data/localizations/usda/2026-04-30/tr.manifest.json
```

The command verifies the exact expected imported generic population: 428 Foundation, 5,431 Survey/FNDDS, and 7,793 SR Legacy foods. Output is sorted by numeric FDC ID and is byte-deterministic for the same dataset and ruleset. The manifest records source-file and artifact SHA-256 values plus coverage and rejection-reason counts.

After the USDA import is present, validate the artifact against PostgreSQL without committing:

```sh
DATABASE_URL='postgres://eatbetter:eatbetter@localhost:5432/eatbetter?sslmode=disable' \
  go run ./cmd/usda-localize load \
  --artifact data/localizations/usda/2026-04-30/tr.jsonl \
  --manifest data/localizations/usda/2026-04-30/tr.manifest.json \
  --dry-run
```

Remove `--dry-run` to perform the atomic, idempotent load. The loader resolves each FDC ID through `external_food_refs`, rejects foods with a GTIN/UPC, and requires exact canonical name and SHA-256 fingerprint agreement. Only `localized` rows are materialized; a scoped `review_required` or `untranslated` result removes an older generated localization.

The USDA importer remains unaware of localization lifecycle. A later canonical-name update can therefore leave a physically present but stale localization. Future readers must compare `food_localizations.source_canonical_name` and `source_fingerprint` with the current canonical name before use, falling back to the USDA `foods.canonical_name` on mismatch. Search and runtime resolution are intentionally outside Phase 4.5.

## Project structure

```text
cmd/api/                    process composition and lifecycle
cmd/usda-import/            USDA bulk import executable
cmd/usda-localize/          deterministic artifact generator and loader
internal/adapters/usda/     provider-specific streaming CSV adapter
internal/application/       focused search, detail, calculation, import, and localization use cases
internal/config/            environment loading and validation
internal/domain/food/       canonical food domain vocabulary and invariants
internal/httpapi/           routes, middleware, and HTTP server configuration
internal/localization/tr/   conservative Turkish glossary and rules
internal/platform/database/ focused PostgreSQL feature adapters and transactional import persistence
migrations/                 versioned schema changes
```

The structure leaves room for feature-oriented domain and application packages in later phases without adding unused repositories, services, factories, or provider abstractions now.

## Technical decisions

- Standard-library `net/http` and `log/slog` keep the foundation small while providing production-relevant server controls and structured JSON logs.
- `pgxpool` owns PostgreSQL connections. Startup verifies connectivity; readiness continues to reflect database availability while liveness remains independent.
- Migrations are an explicit operational step. API replicas therefore cannot race to mutate the schema during startup, and rollbacks remain deliberate.
- The food domain uses fixed MVP nutrition fields rather than generic nutrient rows. This keeps missing-versus-zero semantics explicit without introducing a nutrient catalog before it is needed.
- External source identifiers are separate from canonical food IDs, so future USDA and Open Food Facts adapters can map their DTOs into the same domain.
- Food search queries existing normalized tables directly. It uses no denormalized mirror, cache, external search engine, vector store, embeddings, or runtime AI dependency.
- Food detail uses one food/nutrition/localization query plus one ordered portions query; nutrition calculation loads canonical nutrition and optional owned portion through a focused repository.
- One canonical `foods.id` plus one resolved gram amount produces one backend-owned nutrition result everywhere.
- The process listens only after configuration and PostgreSQL are valid. On `SIGINT` or `SIGTERM`, HTTP traffic drains before the pool closes.
- Docker Compose waits for PostgreSQL's health check and uses a named volume. PostgreSQL 18's volume is mounted at `/var/lib/postgresql`, matching the image's major-version-aware data layout.

## Search evaluation

Run the deterministic 44-query mini evaluation against a populated catalog:

```sh
DATABASE_URL='postgres://...' go run ./cmd/food-search-eval -iterations 3
```

`-summary-only` emits compact metrics and `-failures-only` retains lexical or product-policy misses. Historical Phase 5 measurements remain in [`docs/phase5-search-evaluation.md`](docs/phase5-search-evaluation.md); Phase 6 product-policy and query-plan observations are in [`docs/phase6-product-core.md`](docs/phase6-product-core.md).

## Current boundaries

The current implementation does not include React Native changes, diary persistence, meal history, authentication, user accounts, cloud sync, OpenAI or another LLM, RAG, embeddings, pgvector, AI meal analysis/chat, recommendations, photo recognition, voice input, barcode scanning UI, micronutrient expansion, PDF reports, Redis, queues, WebSockets, background workers, runtime USDA APIs, or Open Food Facts runtime fallback.
