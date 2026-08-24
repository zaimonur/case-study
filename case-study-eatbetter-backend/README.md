# EatBetter Backend

EatBetter Backend; PostgreSQL üzerinde tutulan canonical food catalog, çok dilli ve product-aware food search, food detail, deterministic nutrition calculation ve MealAI yeteneklerini sunan Go tabanlı bir REST API'dir. Metin ve sohbet akışlarında Groq, görsel akışta Gemini yalnızca semantik yorumlama ve yapılandırılmış çıkarım için kullanılır; canonical yiyecek kimliği ve besin değerleri uygulamanın deterministic katmanlarında kalır.

Proje ayrıca USDA FoodData Central bulk import ve deterministik Türkçe yerelleştirme araçlarını, legacy metin/görsel MealAI sözleşmelerini, stateless Chat V2 akışını, smoke araçlarını ve dondurulmuş veri setiyle accuracy evaluation altyapısını içerir.

## Architecture Overview

Temel güven sınırı şöyledir:

```text
Kullanıcı metni / görsel
          │
          ▼
AI semantic interpretation / extraction
  Groq: text + chat   Gemini: image
          │
          ▼
Deterministic candidate search ve identity resolution
          │
          ▼
Deterministic amount resolution
          │
          ▼
Persisted canonical nutrition calculation
          │
          ▼
Güvenilen uygulama sonucu
```

AI provider'ları canonical `FoodID` üretmez veya nutrition truth sahibi değildir. Kimlikler PostgreSQL'deki `foods.id` kayıtlarından, nutrition ise persisted per-100 g verisinden gelir. Yalnızca güvenli ve tekil exact identity evidence otomatik çözülebilir; belirsiz, çoğul ya da eksik kanıt clarification ile fail-closed sonuçlanır. Bu mimaride RAG, embeddings, semantic retrieval veya pgvector yoktur.

## Key Engineering Decisions

- **Canonical food identity tek doğruluk kaynağıdır.** Alias, localization, brand ve provider çıktısı retrieval evidence sağlar; hiçbiri kendi başına yeni bir canonical identity oluşturmaz.
- **Nutrition deterministic katmana aittir.** Güvenilen `FoodID` ile doğrudan gram veya persisted portion seçimi aynı calculation service'e gider; AI kalori ya da makro üretmez.
- **Identity resolution fail-closed çalışır.** Birden fazla güvenli exact identity, güvenli exact eşleşme olmaması veya potansiyel olarak kırpılmış candidate set otomatik seçim yerine clarification üretir.
- **LLM kapsamı dar ve yapılandırılmıştır.** Groq yalnızca purpose, source evidence, food intent ve mevcut clarification bağlamındaki kısıtlı kararı; Gemini yalnızca görüntüde görülen food intent'i çıkarır. Strict JSON Schema ve uygulama katmanı validasyonları provider çıktısını sınırlar.
- **Chat V2 stateless bir continuation sözleşmesidir.** Sunucu durable chat history tutmaz. Client, response'taki `next_state` değerini sonraki istekte aynen geri yollar.
- **Client-carried state güvenilmez girdidir.** Backend state sırasını ve evidence bağlarını doğrular, güncel catalog/portion verisiyle konuşmayı yeniden kurar ve önceki explicit seçimleri tekrar allow-list kontrolünden geçirir.
- **Source order korunur.** Multi-item sonuçlarda ilk unresolved item aktiftir; sonraki bir item çözülebilse bile daha önceki belirsizlik atlanmaz.
- **Assistant metni backend tarafından türetilir.** Kullanıcıya gösterilen cevap, provider prose'u değil materialized state ve güvenilen `NutritionPreview` üzerinden deterministic olarak üretilir.
- **Text/chat ve image provider sınırları ayrıdır.** Groq text/chat yorumlamasını, Gemini image extraction'ı üstlenir; ikisi daha sonra ortak deterministic çözüm yoluna bağlanır.
- **Failure classification açık tutulur.** Configuration, rate limit, timeout, invalid provider output, provider failure, cancellation ve deterministic dependency hataları ayrı HTTP durumlarına eşlenir.

## Technology Stack

| Alan | Teknoloji |
| --- | --- |
| Dil ve HTTP | Go 1.26, standard-library `net/http` |
| Logging | Standard-library `log/slog`, JSON handler |
| Veritabanı | PostgreSQL 18 (local Compose), `pgx` / `pgxpool` |
| Search | PostgreSQL B-tree ifadeleri ve `pg_trgm` GIN index'leri |
| Local runtime | Docker, Docker Compose v2, multi-stage Dockerfile |
| Migration | `migrate/migrate:v4.18.3` container'ı ile explicit migrations |
| Catalog kaynağı | USDA FoodData Central bulk CSV dataset'i |
| Text / chat AI | Groq Chat Completions, strict structured output |
| Image AI | Gemini GenerateContent, structured image-food extraction |

## Quick Start

Gereksinimler:

- Docker ve Docker Compose v2
- Container dışında geliştirme/test için Go 1.26

API başlangıçta migration çalıştırmaz. Yeni bir veritabanı için PostgreSQL'i başlatın, migration'ları açıkça uygulayın, ardından API'yi çalıştırın:

```sh
docker compose up -d db
docker compose run --rm migrate up
docker compose up --build api
```

PostgreSQL health check hazır olduğunda migration ve API adımları devam eder. Varsayılan API origin'i `http://localhost:8080` olur.

Deterministic food/nutrition route'ları için repository'nin Compose development defaults değerleri yeterlidir. AI route'larını kullanmak veya başka ayarları değiştirmek için API'yi başlatmadan önce:

```sh
cp .env.example .env
```

Ardından `.env` içine kendi `GROQ_API_KEY` ve/veya `GEMINI_API_KEY` değerlerinizi ekleyin. Provider key'leri boş bırakıldığında API ayağa kalkar; ilgili AI özelliği `ai_unavailable` döndürür. Development credentials yalnızca yerel kullanım içindir.

Migration lifecycle API'den ayrıdır:

```sh
make migrate-up
make migrate-down
```

Migration set'i canonical food domain, identifiers, localization, `pg_trgm` search altyapısı ve yönetilen retrieval alias metadata'sını kapsar.

## Configuration

Uygulama environment variable'ları `internal/config` tarafından validate edilir. Aşağıdaki default değerler üretim koduna göredir; `.env.example`, local geliştirme kolaylığı için `LOG_LEVEL=debug` örneği verir.

| Variable | Durum / default | Açıklama |
| --- | --- | --- |
| `DATABASE_URL` | API process için **required**; Compose otomatik üretir | PostgreSQL connection URL |
| `APP_ENV` | Optional, `development` | Loglara eklenen runtime environment etiketi |
| `HTTP_PORT` | Optional, `8080` | API listen port; Compose'ta host port mapping'i de belirler |
| `LOG_LEVEL` | Optional, `info` | `debug`, `info`, `warn` veya `error` |
| `HTTP_READ_HEADER_TIMEOUT` | Optional, `5s` | Request header okuma deadline'ı |
| `HTTP_IDLE_TIMEOUT` | Optional, `60s` | Keep-alive idle timeout |
| `SHUTDOWN_TIMEOUT` | Optional, `10s` | Graceful HTTP drain deadline'ı |
| `DB_MAX_CONNS` | Optional, `10` | Maximum PostgreSQL pool size |
| `DB_MIN_CONNS` | Optional, `1` | Minimum pool size; `0` pre-warmed connection'ı kapatır |
| `DB_MAX_CONN_LIFETIME` | Optional, `30m` | Pooled connection maximum lifetime |
| `DB_PING_TIMEOUT` | Optional, `2s` | Startup ve readiness ping deadline'ı |
| `GROQ_API_KEY` | Optional; text/chat AI için required | Groq credential; environment üzerinden verilir |
| `GROQ_MODEL` | Optional, `openai/gpt-oss-120b` | Text/chat provider model configuration |
| `GROQ_TIMEOUT` | Optional, `10s` | Her Groq çağrısı için timeout |
| `GEMINI_API_KEY` | Optional; image AI için required | Gemini credential; environment üzerinden verilir |
| `GEMINI_MODEL` | Optional, `gemini-2.5-flash` | Image provider model configuration |
| `GEMINI_TIMEOUT` | Optional, `15s` | Her Gemini çağrısı için timeout |
| `POSTGRES_USER` | Compose-only, `eatbetter` | Local PostgreSQL user |
| `POSTGRES_PASSWORD` | Compose-only, `eatbetter` | Local PostgreSQL password |
| `POSTGRES_DB` | Compose-only, `eatbetter` | Local database adı |
| `POSTGRES_PORT` | Compose-only, `5432` | Local host port mapping'i |

`POSTGRES_*` değerleri API process configuration'ı değildir; yalnızca Compose içindeki database service ve türetilen `DATABASE_URL` için kullanılır. Repository'deki örnek credentials deployed ortamda kullanılmamalıdır.

## API Overview

| Method ve route | Amaç |
| --- | --- |
| `GET /health` | Process liveness; PostgreSQL'den bağımsız `ok` sonucu |
| `GET /ready` | Configured timeout içinde PostgreSQL ping ile traffic readiness |
| `GET /foods/search` | `q`, opsiyonel `locale` ve `limit` ile sıralı canonical food candidate'ları |
| `GET /foods/{id}` | Canonical food identity, display/localization fallback, per-100 g nutrition ve stored portions |
| `POST /nutrition/calculate` | Doğrudan gram veya explicit stored portion seçimiyle deterministic nutrition |
| `POST /ai/meals/interpret` | Metinden legacy consumed-meal interpretation ve deterministic resolution |
| `POST /ai/meals/interpret-image` | Multipart image'dan visible-food extraction ve deterministic resolution |
| `POST /ai/meals/resolve` | Legacy akışta explicit food, grams veya stored-portion continuation seçimini doğrulama |
| `POST /ai/meals/chat` | Ana conversational text interface: initial turn veya Chat V2 continuation |

`POST /ai/meals/chat` final conversational text yüzeyidir. `interpret` ve `resolve` route'ları backend sözleşmesinin çalışan parçaları olarak kalır; image MealAI kendi `interpret-image` ve deterministic resolve yolunu kullanır.

Food search için örnek:

```sh
curl 'http://localhost:8080/foods/search?q=yumurta&locale=tr-TR&limit=10'
```

Nutrition endpoint'i tam olarak iki moddan birini kabul eder:

```sh
curl -X POST 'http://localhost:8080/nutrition/calculate' \
  -H 'Content-Type: application/json' \
  -d '{"food_id":123,"grams":56}'
```

Stored portion modu `{ "food_id", "portion_id", "quantity" }` alanlarını kullanır. `quantity`, seçilen persisted portion kaydının kaç kez uygulanacağını belirtir; serbest biçimli unit text parse edilmez.

## MealAI Text and Chat Flow

Chat ilk mesajı üç purpose'tan biriyle sınıflandırır:

- `meal_logging`: tüketilen yiyeceği loglama niyeti
- `nutrition_query`: yiyecek için nutrition sorusu
- `unknown`: desteklenen food amacı bulunmayan giriş

Top-level state'ler `ready`, `clarification_required` ve `empty`; clarification türleri `food_identity` ve `amount` değerleridir.

Chat V2 akışı:

1. Groq, ilk mesajdan exact source span'leri source order ile çıkarır; identity-specific kelimeler korunur.
2. Backend her item için deterministic search, identity resolution, amount resolution ve gerekiyorsa nutrition calculation çalıştırır.
3. Response; materialized `items`, ilk unresolved item için `active_item_index`, backend-derived `assistant` ve version `2` `next_state` döndürür.
4. Client sonraki mesajla birlikte dönen `next_state` değerini aynen `state` alanında replay eder.
5. Backend state'i güvenilir kabul etmez: version, purpose, item order, evidence, amount evidence, active index ve explicit seçimleri doğrular; mevcut catalog ve portion verisiyle tüm item'ları yeniden materialize eder.
6. Yalnızca ilk unresolved item için provider kararı istenir. Food identity seçimi mevcut candidate allow-list'iyle, portion seçimi mevcut stored portion ID'leriyle, grams kararı ise son mesajdaki exact gram evidence ile sınırlandırılır.

Continuation kararı güvenli bir candidate `FoodID`, explicit grams, stored portion veya `food_rephrase` olabilir. `food_rephrase`, son mesajdaki exact replacement evidence ile deterministic pipeline'ı yeniden çalıştırır; amount clarification sırasında kullanılamaz. Karar kanıtı yetersizse item unresolved kalır.

Conversation source order korunur ve ilk unresolved item aktif olur. Assistant metni provider'dan alınmaz; trusted materialized state, purpose ve nutrition preview üzerinden Türkçe veya İngilizce üretilir. Chat V2 durable server-side history değildir ve server-side conversation storage içermez.

## Image MealAI Flow

`POST /ai/meals/interpret-image`, `multipart/form-data` içinde `image` ve opsiyonel `locale` kabul eder. JPEG, PNG ve WebP desteklenir; image payload en fazla 8 MiB'dir ve declared MIME type dosya signature'ıyla doğrulanır.

Gemini yalnızca görüntüde görünür food intent'i `observation` ve kısa retrieval query olarak çıkarır. Image extraction quantity veya grams üretmez ve provider sonucu canonical `FoodID` sayılmaz. Her item aynı deterministic identity, amount ve nutrition yoluna girer; kimlik veya miktar belirsizliği clarification üretir ve explicit resolve davranışı kullanılabilir. Image akışı Chat V2 continuation state'inden ayrıdır. Dondurulmuş MealAI accuracy baseline'ı image accuracy ölçmez.

## Food Search and Canonical Resolution

Search, Unicode NFC normalizasyonu, Turkish-aware casing, whitespace/punctuation collapsing ve `ç/ğ/ı/ö/ş/ü` için ayrı ASCII-folded form kullanır. Primary-form eşleşmesi eşdeğer folded eşleşmeden önce gelir.

Retrieval sırası:

1. full-string exact,
2. whole-word / token-sequence,
3. prefix,
4. en az üç karakterli sorgularda gerektiğinde trigram fuzzy fallback.

Canonical name, güncel localized display, localization alias, food alias ve persisted brand ayrı evidence yüzeyleridir. Sonuçlar `foods.id` üzerinde birleştirilir; alias hiçbir zaman canonical identity olmaz. Ranking; match class, primary/folded form, evidence source, leading word konumu, fuzzy similarity ve son tie-breaker olarak `FoodID` ile deterministic'tir. Her retrieval stage `max(40, public_limit × 5)` ile sınırlıdır; public `limit` varsayılan `10`, izin verilen aralık `1..20`'dir.

Product-aware policy, persisted `brand` ve stable `gtin_upc` evidence kullanır. Explicit brand-product sorgusu brand ile product relevance'ı aynı candidate üzerinde arar; branded arama boş dönerse original ordinary query'ye fallback eder, database hatasında fallback yapmaz. Generic/common güçlü sonuçlar branded catalog yoğunluğunda korunur. Stale localization, `source_canonical_name` güncel canonical name ile eşleşmiyorsa retrieval veya display evidence olarak kullanılmaz.

MealAI resolver yalnızca localized display, canonical name, localization alias veya food alias üzerinden gelen tekil güvenli exact identity'yi auto-resolve eder. Birden fazla exact identity, hiç güvenli exact identity olmaması veya candidate sayısının internal default limite ulaşarak kırpılmış olabilmesi clarification üretir. Politika uncertain food seçimine karşı false negative'i tercih eder. Bu trade-off ölçülen MealAI recall sorunlarıyla uyumludur; ancak baseline her miss'in kesin kök nedenini interpretation, retrieval ve resolver arasında ayıracak trace verisini içermez.

## Deterministic Nutrition

```text
canonical FoodID + güvenilir resolved grams
                    │
                    ▼
        deterministic nutrition result
```

`food_nutrition` kayıtları calories, protein, carbohydrates ve fat değerlerini per-100 g olarak saklar. Calculation service ya positive direct grams alır ya da seçilen persisted portion'ın gram değerini positive `quantity` ile çarpar. Önce resolved grams, ardından her bilinen nutrient iki ondalığa deterministic `math.Round` politikasıyla yuvarlanır.

Unknown nutrient JSON'da `null`, bilinen sıfır `0` kalır. Calories makrolardan türetilmez. Serbest measure parsing, tipik porsiyon tahmini, density inference ve desteklenmeyen ml-to-gram conversion yapılmaz. MealAI yalnızca trusted identity ve amount yolunu kullanır; **AI nutrition hesaplamaz**.

## USDA Import and Turkish Localization

`cmd/usda-import`, extracted USDA FoodData Central full CSV dataset'ini bounded-memory streaming ile okur. Required header ve nutrient tuple'larını doğrular, canonical-shaped temporary table'lara PostgreSQL `COPY` yapar ve advisory lock altındaki transaction ile atomik merge gerçekleştirir; kalıcı raw USDA mirror oluşturmaz.

Generic Foundation, FNDDS ve SR Legacy kayıtları USDA FDC ID ile; branded kayıtlar exact GTIN/UPC text ile stable identity kazanır. Branded bir ürünün seçilen güncel payload'ı canonical kaydı güncellerken gözlenen FDC ID'ler external reference olarak korunur. Persisted nutrition missing-versus-zero ayrımını korur. Güvenilir USDA portion kayıtları saklanır; source-native ölçü açıklamaları parse edilmez ve branded `ml` serving için gram tahmini yapılmaz.

Örnek import:

```sh
DATABASE_URL='postgres://eatbetter:eatbetter@localhost:5432/eatbetter?sslmode=disable' \
  go run ./cmd/usda-import \
  --dataset-dir '/path/to/FoodData_Central_csv_2026-04-30' \
  --dataset-date 2026-04-30
```

Turkish localization aracı branded foods'u çevirmeden repository-controlled glossary/rule set ile deterministic JSONL artifact üretir. Loader; artifact hash/manifest, external reference, canonical name ve source fingerprint eşleşmesini doğrular; branded identity'yi reddeder ve load işlemini atomik/idempotent yürütür. Canonical name daha sonra değişirse fiziksel localization satırı kalabilse de runtime search/detail stale row'u kullanmaz ve canonical name'e fallback eder.

```sh
go run ./cmd/usda-localize generate \
  --dataset-dir '/path/to/FoodData_Central_csv_2026-04-30' \
  --dataset-date 2026-04-30 \
  --output data/localizations/usda/2026-04-30/tr.jsonl \
  --manifest data/localizations/usda/2026-04-30/tr.manifest.json

DATABASE_URL='postgres://eatbetter:eatbetter@localhost:5432/eatbetter?sslmode=disable' \
  go run ./cmd/usda-localize load \
  --artifact data/localizations/usda/2026-04-30/tr.jsonl \
  --manifest data/localizations/usda/2026-04-30/tr.manifest.json \
  --dry-run
```

## Observability and Failure Handling

- API ve data tooling, `slog` JSON logları üretir. Access log; generated request ID, method, path, status ve duration içerir. Request body, chat state, provider credential ve database connection string loglanmaz.
- Her handled response `X-Request-ID` taşır. Panic recovery, client'a internal detail sızdırmadan `internal_error` döndürür.
- `/health` process liveness, `/ready` bounded PostgreSQL ping ile readiness sağlar.
- `SIGINT` ve `SIGTERM` graceful shutdown başlatır; configured deadline aşılırsa server kapatılır ve ardından pool kapanır.
- JSON body'leri bounded ve strict decode edilir; unknown field veya trailing JSON reddedilir. Nutrition 4 KiB, interpret 32 KiB, resolve 16 KiB, chat 48 KiB body limitine sahiptir; image upload ayrıca 8 MiB image ve bounded multipart overhead uygular.
- Bütün AI route response'larına `Cache-Control: no-store` eklenir.
- Provider çağrıları request context'i ve provider-specific timeout ile bounded'dır; response body'leri de sınırlıdır. Cancellation ve timeout ayrı sınıflandırılır.
- Provider `401/403`, `429`, invalid structured output ve diğer provider failure durumları ayrı application/HTTP error türlerine çevrilir. Backend kendi global rate limiter'ını uygulamaz; provider'ın rate-limit cevabını güvenli şekilde sınıflandırır.

APM, distributed tracing veya production monitoring infrastructure bu repository kapsamında yoktur.

## Testing and Validation

Ana deterministic doğrulama komutu:

```sh
make verify
```

Bu target `go test -race ./...`, `go vet ./...` ve API, importer, localizer, search evaluator, legacy/chat smoke ve MealAI evaluator binary build'lerini çalıştırır. External provider çağrısı yapmaz. PostgreSQL integration testleri `TEST_DATABASE_URL` verilmediğinde skip edilir; gerçek catalog query-plan testleri ayrıca `REAL_CATALOG_DATABASE_URL` ister.

Test yüzeyleri şunları kapsar:

- deterministic domain/application unit testleri,
- PostgreSQL repository/integration testleri,
- HTTP route ve strict contract testleri,
- Groq/Gemini adapter schema, timeout, cancellation ve error-classification testleri,
- legacy ve Chat V2 smoke tooling contract testleri,
- frozen evaluation dataset ve evaluator invariants.

Live komutlar çalışan local API, populated catalog ve ilgili provider configuration gerektirir; `make verify` bunları execute etmez:

```sh
make ai-smoke
make ai-chat-smoke
make ai-eval
```

Final Chat V2 automated/live smoke ve manual mobile acceptance özeti için [`PHASE15_CHAT_ACCEPTANCE.md`](PHASE15_CHAT_ACCEPTANCE.md) belgesine bakın. Acceptance matrisi accuracy baseline'ının yerine geçmez; iki yüzey farklı amaçları ölçer.

## Search Evaluation

En güncel real-catalog product-core snapshot'ı 2026-08-20 tarihinde 473,999 canonical food ve genişletilmiş 44-query evaluator üzerinde ölçüldü:

| Metric | Sonuç |
| --- | ---: |
| Top-1 family hits | 32 / 37 (86.5%) |
| Top-5 family recall | 34 / 37 (91.9%) |
| Expected no-result correctness | 3 / 3 (100%) |
| Product-policy success | 4 / 4 (100%) |
| End-to-end latency | p50 124.3 ms; p95 960.5 ms |

Bu snapshot yeniden üretilmiş güncel bir benchmark değil, committed ölçüm kanıtıdır. [`docs/phase5-search-evaluation.md`](docs/phase5-search-evaluation.md) önceki 42-query lexical search ölçümünü; [`docs/phase6-product-core.md`](docs/phase6-product-core.md) ise sonraki 44-query product-policy snapshot'ını içerir. İki ölçümün denominator ve latency değerleri karıştırılmamalıdır.

Populated catalog üzerinde evaluator komutu:

```sh
DATABASE_URL='postgres://...' go run ./cmd/food-search-eval -iterations 3 -summary-only
```

Bu komut normal test suite'inin parçası değildir ve committed ölçümü yeniden üretmeden önce dataset/catalog provenance ayrıca doğrulanmalıdır.

## MealAI Accuracy Evaluation

Immutable `mealai-chat-v1` baseline; 30 case, 34 turn, 30/30 evaluable case ve `0` infrastructure-error case içeren ilk `COMPLETE` ölçümdür:

| Primary metric | Sonuç |
| --- | ---: |
| Canonical resolution accuracy | 11 / 31 (35.5%) |
| Amount accuracy | 8 / 24 (33.3%) |
| Clarification correctness | 16 / 34 (47.1%) |
| Unsafe auto-resolution rate | 0 / 7 (0.0%) — lower is better |
| End-to-end success | 14 / 30 (46.7%) |

Evaluation set, Task 6 kapsamındaki herhangi bir measured evaluation run'dan önce donduruldu; Task 6 evaluator sonuçlarına tepki olarak label'lar değiştirilmedi. Bu ifade label'ların daha önceki tüm manual veya smoke gözlemlerinden önce oluşturulduğu anlamına gelmez. İlk `COMPLETE` baseline değişmeden kabul edildi; o `COMPLETE` sonucu iyileştirmek için production behavior, label'lar, metric contract veya evaluator tune edilmedi.

Başarısız vakaların çoğu frozen canonical identity'ye ulaşamadı. Artefakt provider'ın extracted query'sini veya candidate listesini saklamadığından her hatayı tek başına resolver'a atfetmek mümkün değildir. `0/7` unsafe auto-resolution yalnızca yedi frozen safety-labeled turn üzerindeki bounded sonuçtur; universal safety iddiası değildir. Committed baseline belirli bir model label kaydetmediği için bu ölçüm configured default modele atfedilmez.

Detaylı methodology, failure taxonomy ve limitations için [`docs/phase15-ai-accuracy-evaluation.md`](docs/phase15-ai-accuracy-evaluation.md); immutable ölçüm için [`data/evaluation/results/mealai-chat-v1-baseline.json`](data/evaluation/results/mealai-chat-v1-baseline.json) dosyasına bakın.

## Trade-offs and Known Limitations

- Conservative exact identity resolution, uncertain auto-selection riskini azaltırken recall'ı düşürür; measured canonical resolution sonucu bu maliyeti açıkça gösterir.
- Search lexical ve deterministic'tir; semantic retrieval, embeddings, pgvector ve RAG uygulanmamıştır.
- Durable server-side chat history ve streaming chat response yoktur. Client-carried continuation state her turn yeniden doğrulanır fakat conversation store değildir.
- Bu case-study backend kapsamında authentication, user account veya authorization altyapısı yoktur.
- `mealai-chat-v1` yalnızca 30 curated case/34 turn içerir; 29 vaka `tr-TR`, 1 vaka `en-US` olduğundan sonuçlar küçük ve Turkish-heavy bir snapshot'tır.
- Dondurulmuş accuracy evaluation image accuracy'yi kapsamaz; portion, density veya volume estimation benchmark'ı da içermez.
- Image extraction miktarı ölçmez. Kullanıcı explicit grams veya trusted stored portion sağlamadığında clarification gerekir.
- External AI provider'ları latency, availability, quota/rate-limit ve model-behavior riski ekler.
- Search fuzzy ve broad query'lerinde tail latency yükselebilir; committed snapshot'ta p95 960.5 ms'dir.
- Sistem production monitoring, authentication ve deployment hardening içeren production-ready infrastructure iddiasında bulunmaz.

## What I Would Improve Next

1. **Candidate recall:** Curated multilingual aliases, typo-tolerant retrieval ve hard-negative testleri genişletmek; ardından canonical `FoodID` truth ve fail-closed seçim politikasını koruyan evaluated semantic candidate retrieval araştırmak.
2. **Ordered multi-food behavior:** Partial resolution ve first-unresolved etkileşimini daha görünür hale getirmek, çoklu yiyecek regressions'ını artırmak ve ölçümdeki `0/3` multi-food sonucunu hedeflemek.
3. **Evaluation coverage:** Bağımsız biçimde yeniden dondurulmuş daha geniş Turkish-heavy corpus, daha fazla continuation, portion ve image case'i eklemek; model/provider metadata ve güvenli trace evidence ile interpretation–retrieval–resolver ayrımını ölçülebilir kılmak.

Semantic retrieval veya pgvector bu maddelerde future retrieval work'tür; mevcut uygulamanın parçası değildir.

## Project Structure

```text
cmd/api/                         API composition ve process lifecycle
cmd/usda-import/                 USDA bulk importer
cmd/usda-localize/               deterministic localization generator/loader
cmd/food-search-eval/            real-catalog lexical/product evaluator
cmd/meal-ai-smoke/               legacy text/resolve live smoke tool
cmd/meal-ai-chat-smoke/          Chat V2 live smoke tool
cmd/meal-ai-eval/                frozen conversational accuracy evaluator

internal/adapters/groq/          text/chat structured provider adapter
internal/adapters/gemini/        image extraction provider adapter
internal/adapters/usda/          streaming USDA CSV adapter

internal/application/mealai/     text, image, chat orchestration ve assistant output
internal/application/mealchat/   provider-independent chat contracts/validation
internal/application/foodsearch/ normalization ve candidate search policy
internal/application/foodresolver/ fail-closed canonical identity policy
internal/application/foodamount/ deterministic amount/portion resolution
internal/application/nutritioncalc/ persisted nutrition calculation

internal/httpapi/                routes, DTO mapping, limits ve middleware
internal/platform/database/      pgx repositories ve transactional persistence
internal/domain/food/            provider-independent canonical food domain

data/evaluation/                 frozen dataset ve immutable baseline
migrations/                      versioned PostgreSQL schema changes
docs/                            detailed measurement/evaluation evidence
```
