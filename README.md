# EatBetter — Full Stack Developer Case Study

Bu case study'nin odağı **Meal Logging Accuracy**: serbest metin veya yemek görseli gibi dağınık girdileri doğru canonical yiyecek, doğru miktar/porsiyon ve güvenilir nutrition sonucuna dönüştürmek. Sistem bunu tek başına bir LLM'e bırakmak yerine AI yorumlama ile deterministic retrieval, çözümleme ve hesaplama katmanlarını birleştiren hybrid bir yaklaşım kullanır.

Groq metin ve sohbet girdilerini semantik olarak yorumlar; Gemini görseldeki yiyecekleri çıkarır. Hiçbir AI provider canonical `FoodID` veya güvenilen kalori/makro değerleri üretmez. Bu sorumluluklar backend'deki persisted catalog ve deterministic katmanlara aittir; belirsizlik sessiz bir eşleşme yerine clarification doğurur. React Native / Expo uygulaması bu akışı manuel kayıt, text/image MealAI, review ve cihazdaki günlük deneyimiyle uçtan uca sunar.

## Ne geliştirdim?

- **Backend:** Go REST API, PostgreSQL canonical food catalog, USDA import/Türkçe localization araçları, food search/detail, deterministic nutrition, text MealAI, conversational Chat V2, image MealAI ve smoke/evaluation tooling.
- **Mobile:** React Native / Expo uygulaması, manuel yiyecek kaydı, text ve image MealAI, device-local günlük, 7 günlük analiz ve PDF export/share.

## Meal Logging Accuracy yaklaşımı

```text
Dağınık metin / görsel
          ↓
AI semantic interpretation / extraction
          ↓
Deterministic candidate retrieval
          ↓
Fail-closed canonical identity resolution
          ↓
Deterministic miktar / porsiyon çözümleme
          ↓
Deterministic nutrition
          ↓
Clarification VEYA güvenilen review sonucu
          ↓
Kullanıcının açık kaydetme işlemi
          ↓
Yerel MealRecord
```

LLM provider'ları catalog dışında kimlik uyduramaz; canonical kimlik yalnızca PostgreSQL'deki kayıtlı food catalog'dan gelir. Miktar, persisted portion ve nutrition sonucu backend tarafından materialize edilir. Tekil ve güvenli identity evidence veya yeterli miktar yoksa akış fail-closed davranır ve kullanıcıdan netleştirme ister. Hazır sonuç kaydedilmeden önce kullanıcıya gösterilir; yalnızca açık onaydan sonra cihazda saklanır.

MealAI'dan bağımsız deterministic bir yol da vardır: kullanıcı catalog'da arama yapar, canonical food ile gram/porsiyon seçer, backend hesabını inceler ve günlüğe ekler. Böylece temel meal logging deneyimi AI provider erişilebilirliğine bağlı değildir.

## Accuracy değerlendirmesi

Dondurulmuş `mealai-chat-v1` setinin ilk `COMPLETE` baseline'ı 30 case ve 34 turn içerir. 30/30 case evaluable olmuş, çözümlenmemiş infrastructure-error sayısı `0` kalmıştır.

| Primary metric | Sonuç |
| --- | ---: |
| Canonical resolution accuracy | 11 / 31 (%35,5) |
| Amount accuracy | 8 / 24 (%33,3) |
| Clarification correctness | 16 / 34 (%47,1) |
| Unsafe auto-resolution rate | 0 / 7 (%0,0) — düşük olması daha iyi |
| End-to-end success | 14 / 30 (%46,7) |

Bu baseline production-grade veya yüksek accuracy göstermez. Özellikle canonical recall temel zayıflıktır; kimlik bulunamadığında downstream miktar ve E2E sonuçları da etkilenir. Conservative resolver, belirsiz auto-selection riskini azaltmak için recall'dan ödün verir. `0/7` unsafe auto-resolution yalnızca safety etiketi taşıyan yedi turn üzerindeki sınırlı kanıttır; evrensel bir güvenlik iddiası değildir.

Evaluation set ölçülen baseline çalıştırmalarından önce donduruldu ve ilk `COMPLETE` baseline değiştirilmeden kabul edildi. Bu sonuç daha iyi görünsün diye ürün davranışı, label'lar, metric contract veya evaluator sonradan tune edilmedi. Methodology, failure taxonomy ve sınırlar için [MealAI Accuracy Evaluation](case-study-eatbetter-backend/docs/phase15-ai-accuracy-evaluation.md), immutable sonuç için [baseline artefaktı](case-study-eatbetter-backend/data/evaluation/results/mealai-chat-v1-baseline.json) incelenebilir.

## En büyük trade-off

Temel trade-off **safety/precision ile recall** arasındadır. Yanlış bir canonical food'un otomatik seçilmesi sessizce yanlış nutrition üretebilir; bu nedenle resolver belirsiz automatic selection yerine clarification/false negative'i tercih eder. Sonuç daha güvenli, sınırlı auto-resolution; karşılığında daha düşük recall ve zayıf canonical-resolution baseline'ıdır.

## Mimari sınırlar

- AI katmanı semantic interpretation/extraction yapar; deterministic backend canonical food, miktar/porsiyon ve nutrition truth'ün sahibidir.
- Backend food catalog ve hesaplama kaynağıdır; mobile presentation, contract validation, kullanıcı onayı ve yerel persistence'ı yönetir.
- Device-local `MealRecord` günlüğü backend canonical catalog'dan ayrıdır; backend'de kullanıcı günlüğü tutulmaz.
- Görünen chat transcript'i ile opaque continuation state ayrı tutulur; mobile state'ten yeni food/nutrition kararı türetmez.
- Text Chat V2 stateless continuation contract'ını, image MealAI ise ayrı image/resolve contract'ını kullanır.

Uygulama düzeyi ayrıntılar [Backend README](case-study-eatbetter-backend/README.md) ve [Mobile README](case-study-eatbetter-mobile/README.md) belgelerindedir.

## Reliability ve Observability

- Backend strict request/provider schema validation, bounded body/response, provider timeout/cancellation ve provider/rate-limit hata sınıflandırması uygular.
- Request ID, structured JSON access log, health/readiness ve AI response'larında `Cache-Control: no-store` davranışı bulunur.
- Mobile aynı anda tek aktif chat isteği, `AbortController`, stale-response ownership guard'ları ve başarısız turn için transactional state davranışı kullanır.
- Retry, başarısız turn'ün exact message/locale/continuation snapshot'ından yapılır; duplicate-save guard'ları ve local persistence validation uygulanır.

Bu case-study APM, distributed tracing veya production monitoring infrastructure içermez; mevcut observability kapsamı log, request correlation ve health/readiness yüzeyleriyle sınırlıdır.

## Bilinçli olarak kapsam dışında bıraktıklarım

Semantic retrieval, embeddings, pgvector, RAG, durable server-side chat history, streaming chat, authentication/account backend, cloud/mobile diary synchronization, image-accuracy benchmark, exhaustive Android/tablet/web QA ve dedicated automated mobile test framework uygulanmadı.

Yedi günlük timebox; geniş özellik yüzeyi yerine uçtan uca ürün akışını, deterministic trust boundary'lerini, ambiguity handling'i, reliability'yi ve ölçülebilir accuracy'yi önceliklendirdi.

## Accuracy'yi sonraki üç adımda nasıl iyileştirirdim?

1. **Candidate recall:** Curated multilingual alias'lar, typo-tolerant retrieval ve hard-negative coverage ekler; ardından mevcut fail-closed sınırı koruyan semantic candidate retrieval'ı kontrollü olarak değerlendirirdim.
2. **Multi-food / continuation:** Ordered partial resolution, first-unresolved davranışı ve daha geniş multi-item/continuation regression coverage geliştirirdim.
3. **Evaluation coverage:** Daha büyük ve bağımsız dondurulmuş corpus'a continuation ve image case'leri ekler; interpretation, retrieval ve resolver ayrımını gösterecek daha iyi trace evidence toplardım.

Semantic retrieval, embeddings ve pgvector burada future work'tür; mevcut sistemde uygulanmış değildir.

## Scale'de ilk nereler zorlanır?

1. **Lexical search tail latency:** Broad/fuzzy sorgular mevcut retrieval katmanındaki ilk tail-latency baskısıdır.
2. **External AI provider'ları:** Latency, quota, rate limit ve availability uçtan uca deneyimi sınırlar.
3. **Stateless Chat V2 reconstruction:** State'i her turn yeniden doğrulamak mevcut kapsamda basit ve sağlamdır; uzun konuşmalar ve büyük multi-food session'ları turn başına işi artırır.
4. **Device-local diary:** Multi-device ürün kapsamı authenticated backend meal persistence ve synchronization gerektirir.

Ölçümle gerekçelendirilmiş semantic candidate retrieval/cache, provider abstraction/fallback, signed veya opaque server-issued continuation state ve authenticated meal persistence olası gelecek yönleridir; mevcut production-scale yetenekler olarak sunulmaz.

## Security ve Privacy

Provider credentials yalnızca server-side environment configuration'da kalır. Provider çıktısı canonical `FoodID`/nutrition truth sayılmaz; structured output doğrulanır. Request body'leri sınırlandırılır, image type ve file signature kontrol edilir. Client-carried Chat V2 state güvenilmez input kabul edilerek yeniden doğrulanır. Normal uygulama loglarına request/chat payload'ları, provider credentials veya database connection string yazılmaz. Mobile diary kayıtları cihazda yerel kalır.

Başlıca riskler şunlardır: case-study kapsamında authentication/authorization yoktur; continuation state doğrulanır fakat cryptographically signed veya opaque değildir; ilgili kullanıcı metni ve görselleri external AI provider'larına gönderilir. Production kullanımı için açık consent, privacy ve data-retention politikalarının ayrıca tasarlanması gerekir.

## Çalıştırma

1. Backend'i migration ve provider configuration adımlarıyla başlatmak için [Backend README](case-study-eatbetter-backend/README.md) izlenmelidir.
2. Mobile dependency kurulumu, API origin ayarı ve Expo başlangıcı [Mobile README](case-study-eatbetter-mobile/README.md) içinde belgelenmiştir.
3. Mobile runtime'ın yapılandırılan backend origin'ine ağ üzerinden erişebilmesi gerekir.

## Doğrulama özeti

Repository'de kaydedilmiş final kanıtları:

| Yüzey | Sonuç |
| --- | --- |
| Backend `make verify` | PASS |
| Mobile `npm run typecheck` | PASS |
| Mobile `npx expo-doctor` | PASS — 21/21 |
| Chat live smoke | 5 required PASS, 0 FAIL, 1 optional SKIPPED, 0 required failure |

Manual mobile acceptance; nutrition query, logging, amount continuation, multi-food, retry/reset, text-image lifecycle, image regression ve bounded keyboard/scroll senaryolarını kapsar. Ayrıntılar ve açık `SKIPPED` sınırı [Chat V2 Acceptance](case-study-eatbetter-backend/PHASE15_CHAT_ACCEPTANCE.md) belgesindedir.

## Teknik detaylar

```text
case-study/
├── case-study-eatbetter-backend/
└── case-study-eatbetter-mobile/
```

- [Backend README](case-study-eatbetter-backend/README.md)
- [Mobile README](case-study-eatbetter-mobile/README.md)
- [MealAI Accuracy Evaluation](case-study-eatbetter-backend/docs/phase15-ai-accuracy-evaluation.md)
- [Chat V2 Acceptance](case-study-eatbetter-backend/PHASE15_CHAT_ACCEPTANCE.md)

## AI araçlarını nasıl kullandım?

### Runtime AI

- **Groq:** Text/chat semantic interpretation.
- **Gemini:** Görselden yiyecek extraction.

Bu provider'ların güven sınırları yukarıdaki mimari bölümünde tanımlanmıştır.

### Development AI tools

ChatGPT / Codex; architecture tartışması, implementation desteği, code review, debugging/iteration, test/evaluation tasarımı ve dokümantasyon iyileştirmesinde development assistant olarak kullanıldı. Engineering ve trade-off kararları developer-owned kaldı; implementation iddiaları repository ile, validation/evaluation kanıtları ayrı artefaktlarla kontrol edildi. Manual acceptance bağımsız bir doğrulama yüzeyi olarak korundu.
