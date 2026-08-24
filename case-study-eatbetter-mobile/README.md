# EatBetter Mobile

EatBetter Mobile, React Native ve Expo ile geliştirilmiş bir beslenme günlüğü case-study uygulamasıdır. Kullanıcılar backend food catalog içinde arama yapabilir, gram veya kayıtlı porsiyon üzerinden besin değerini hesaplatabilir ve sonucu cihazdaki günlüğe kaydedebilir. Uygulama ayrıca conversational text MealAI, image MealAI, son yedi güne ait beslenme analizi ve yerel PDF raporu üretme/paylaşma akışlarını içerir.

MealAI sonuçları doğrudan kaydedilmez: belirsiz yiyecek kimliği veya miktarı önce netleştirilir, hazır sonuç kullanıcıya gösterilir ve yalnızca açık kaydetme işlemi sonrasında yerel `MealRecord` oluşturulur.

## Özellikler

- Backend food catalog içinde Türkçe arama ve canonical food detail görüntüleme
- Doğrudan gram veya backend'in sunduğu kayıtlı porsiyonlarla besin değeri hesaplama
- Hesaplanan öğünü cihazdaki yerel günlüğe kaydetme
- Bugünün öğünlerini, kalori ve makro toplamlarını görüntüleme; bugünkü kayıtları onayla silme
- Bugün dahil son yedi yerel takvim gününü kapsayan analiz, kalori görünümü ve veri kapsamı özeti
- Yedi günlük veriden cihazda PDF raporu üretme ve platform share sheet ile paylaşma
- `POST /ai/meals/chat` üzerinden conversational text MealAI:
  - besin değeri soruları,
  - AI destekli öğün kaydı,
  - çoklu yiyecek,
  - yiyecek kimliği ve miktar netleştirmesi,
  - başarısız turn için retry
- Kamera veya galeriden görsel alma, görseli hazırlama ve multipart image MealAI isteği gönderme
- Image MealAI sonucunda yiyecek/miktar netleştirmesi, review ve yerel günlük kaydı

Profil sekmesi yalnızca placeholder ekrandır; authentication, account veya tamamlanmış bir profil sistemi değildir.

## Teknoloji Stack'i

| Alan | Teknoloji |
| --- | --- |
| Mobile runtime | React Native `0.86.2` |
| Uygulama framework'ü | Expo `~57.0.16` |
| Navigation | Expo Router `~57.0.16` |
| UI | React `19.2.3` |
| Dil | TypeScript `~6.0.3`, strict mode |
| Yerel veri | `@react-native-async-storage/async-storage` `2.2.0` |
| Görsel girişi | Expo Image Picker, Expo Image Manipulator |
| Dosya ve rapor | Expo File System, Expo Print, Expo Sharing |
| Uzak bağımlılık | HTTP üzerinden Go/PostgreSQL backend |

## Mimari

Proje, isimlendirilmiş bir framework mimarisi iddiası yerine sorumlulukları dosya sınırlarıyla ayırır:

- `app/`: Expo Router route'ları, ekran composition'ı ve navigation.
- `src/api/`: HTTP transport, request serialization, response shape doğrulama ve hata sınıflandırması.
- `src/domain/`: Food, nutrition, meal, analysis, report ve MealAI mobil contract'ları.
- `src/storage/`: `MealRecord[]` verisinin `AsyncStorage` ile okunması/yazılması.
- `src/state/`: Uygulama genelindeki meal hydration ve sıralı write ownership'i.
- `src/features/`: Home, food, MealAI, analysis ve report'a özgü UI/session/logic.

Güven ve sahiplik sınırı şöyledir:

```text
Mobildeki kullanıcı niyeti
          ↓
Backend HTTP API
          ↓
Canonical FoodID + güvenilen nutrition sonucu
          ↓
Mobil review/rendering + yerel MealRecord persistence
```

Backend-backed manual food ve MealAI akışlarında mobil uygulama canonical `FoodID` veya güvenilen kalori/makro değerlerini üretmez. Bunlar backend'in catalog ve deterministic nutrition katmanından gelir. Mobil taraf; kullanıcı seçimini toplar, contract'ı doğrular, sonucu gösterir ve kullanıcı onayından sonra yerel günlüğe yazar.

## Quick Start

Backend kurulumu ve çalıştırma adımları için [EatBetter Backend README](../case-study-eatbetter-backend/README.md) belgesini izleyin. Mobile uygulamayı başlatmadan önce backend çalışıyor ve seçilen runtime'dan erişilebilir olmalıdır.

Committed lockfile ile temiz kurulum:

```sh
npm ci
```

API origin'ini belirleyip Expo development server'ı başlatın. Aşağıdaki placeholder'ı çalıştırmadan önce gerçek, erişilebilir backend host'u ile değiştirin:

```sh
EXPO_PUBLIC_API_BASE_URL="http://<device-reachable-backend-host>:8080" npm start
```

Mevcut script'ler:

```sh
npm start
npm run ios
npm run android
npm run typecheck
```

`EXPO_PUBLIC_API_BASE_URL`, path içermesi gerekiyorsa o path'i de içeren bir `http://` veya `https://` origin/base URL olmalıdır. Değer yoksa, geçerli bir URL değilse ya da HTTP(S) dışında bir protocol kullanıyorsa client açık bir configuration error üretir.

`localhost` her runtime için aynı makineyi ifade etmez. Simulator/emulator network davranışı platforma göre değişir; fiziksel cihaz backend'e cihazdan erişilebilen bir origin ister ve bunun için development makinesinin LAN adresi gerekebilir.

### Platform kapsamı

Proje Expo üzerinden iOS ve Android komutlarını sağlar; `app.json` iOS tablet desteğini yapılandırır ve bağımlılıklarda React Native Web bulunur. Bunlar tek başına exhaustive Android, tablet veya web ürün doğrulaması anlamına gelmez. Kaydedilmiş final mobile acceptance kanıtı belirli senaryolarla sınırlıdır.

## Backend Bağlantısı

Food search/detail, nutrition calculation ve MealAI verileri `EXPO_PUBLIC_API_BASE_URL` ile seçilen backend HTTP API'den gelir. Mobil README backend environment variable'larını veya endpoint uygulama ayrıntılarını tekrar etmez; bunlar backend belgesinin sorumluluğundadır.

`src/api/client.ts`:

- base URL'yi normalize eder ve yalnızca HTTP(S) kabul eder;
- configuration, network, HTTP, invalid JSON/response ve MealAI timeout hatalarını ayrı tutar;
- backend'in `X-Request-ID` header'ını `ApiError` metadata'sında korur;
- cancellation'ı network failure'a dönüştürmeden üst katmana taşır.

Food, nutrition ve MealAI API modülleri başarılı response'ları mobil domain contract'larına çevirmeden önce shape ve temel invariant doğrulaması yapar. MealAI istekleri ayrıca 30 saniyelik client timeout ile sınırlandırılır.

## Manuel Yiyecek Kaydı

```text
Food search
    → canonical food seçimi
    → detail + per-100 g referans + kayıtlı porsiyonlar
    → gram veya desteklenen porsiyon seçimi
    → backend nutrition calculation
    → hesaplanan sonucu review
    → yerel MealRecord
```

Arama ekranı en az iki karakter ister, 300 ms debounce uygular ve yeni sorgu geldiğinde eski isteği iptal eder. Seçilen `FoodID`, backend arama sonucundan gelir; detail ekranı aynı kimliğin canonical adını, display adını, brand bilgisini, per-100 g nutrition referansını ve stored portion seçeneklerini yükler.

Kullanıcı ya pozitif gram değeri ya da backend'in sunduğu bir `portionId` ile pozitif quantity seçer. Mobil client trusted calories/macros hesaplamaz; seçimi `POST /nutrition/calculate` isteğine dönüştürür ve backend'in döndürdüğü `resolvedGrams` ile nutrition sonucunu review kartında gösterir. Seçim değişirse önceki hesaplama geçersizleştirilir. Kullanıcının `Günlüğe Ekle` onayından sonra tek item'lı yerel `MealRecord` oluşturulur.

Kaydedilmiş öğün için edit akışı yoktur. Günlük ekranında yalnızca bugünün görünen kayıtları kullanıcı onayından sonra silinebilir.

## Yerel Persistence

Öğün günlüğü `AsyncStorage` içinde `eatbetter.meals.v1` anahtarında JSON olarak saklanır. Persist edilen her `MealRecord` şunları içerir:

- client tarafından üretilen yerel `id`;
- ISO 8601 `loggedAt` timestamp'i;
- bir veya daha fazla `MealItem`;
- her item için canonical/display food alanları, resolved grams, nutrition sonucu ve gram/portion seçim snapshot'ı.

Uygulama açıldığında store hydrate edilir ve veri şekli kullanılmadan önce doğrulanır. Hydration tamamlanmadan write yapılmaz; aynı anda yalnızca bir add/remove write kabul edilir. Write önce `AsyncStorage`'a tamamlanır, ardından in-memory state güncellenir.

Bu kayıtlar local-only'dir. Backend diary persistence, cloud backup, account sync veya multi-device synchronization yoktur. Yerel gün gruplaması `loggedAt` değerinin cihazın local calendar gününe çevrilmesiyle yapılır.

Chat meal save retry sırasında aynı hazırlanmış `MealRecord` ve kimliği yeniden kullanılır; ayrıca in-flight ve success guard'ları aynı aktif review'dan tekrarlı save'i engeller.

## MealAI Text Chat

Text modu, her free-text turn için `POST /ai/meals/chat` kullanır. Backend response'u şu üç amacı ayırır:

- `meal_logging`: review sonrasında yerel günlüğe kaydedilebilen öğün sonucu;
- `nutrition_query`: ekranda gösterilen read-only besin sonucu;
- `unknown`: desteklenen bir amaç bulunmadığında guidance/empty sonucu.

Response; backend tarafından oluşturulan assistant içeriğini, materialized item/result verisini ve `next_state` değerini taşır. Mobil uygulama assistant metnini yeniden üretmez; backend'in doğrulanmış metnini gösterir.

Görünen transcript ile backend continuation state iki ayrı state yüzeyidir. `next_state` mobil domain katmanında contract olarak doğrulanır ve semantik olarak opaque kabul edilerek sonraki clarification turn'ünde geri gönderilir; mobil taraf bu state'ten yeni `FoodID`, yiyecek sırası veya nutrition kararı türetmez. Quick-choice chip'leri de özel bir local resolve uygulamak yerine normal chat mesajı gönderir.

Yalnızca `clarification_required` sonucu continuation state'i korur. `ready` ve `empty` terminal sonuçları state'i temizler. `nutrition_query` sonucu read-only'dir ve meal olarak persist edilemez. `meal_logging` sonucu tüm item'lar `ready` olduğunda ortak review/yerel `MealRecord` yoluna girebilir.

Chat V2 streaming değildir ve transcript kalıcı storage'a yazılmaz.

### Çoklu yiyecek ve clarification

Mobil taraf backend'in item/source sırasını korur. Backend'in `active_item_index` değeri authoritative'dir; yalnızca aktif unresolved item için clarification etkileşimi sunulur. Sonraki belirsiz item'lar ekranda bekleyen durum olarak gösterilir. Mobil uygulama item'ları yeniden sıralamaz veya belirsiz yiyecekleri sessizce çözmez.

Yiyecek kimliği clarification'ı backend'in candidate seçenekleri veya kullanıcının yeniden ifadesiyle, miktar clarification'ı ise free-text gram/porsiyon yanıtıyla ilerler. Bunlar ayrı belirsizlik türleridir.

## Chat Session Güvenilirliği

Chat session aşağıdaki invariant'ları uygular:

- Aynı anda yalnızca bir aktif request/turn vardır.
- Her istek `AbortController` sahibidir; reset veya unmount aktif isteği iptal eder.
- Session generation, request token ve turn ID ownership kontrolleri stale response'un yeni session'a commit edilmesini engeller.
- Başarısız turn'de kullanıcı mesajı yalnızca bir kez transcript'e eklenir; başarısız assistant mesajı commit edilmez.
- Retry yeni bir kullanıcı mesajı oluşturmaz; ilk istekte yakalanan özgün message, locale ve continuation snapshot'ını yeniden kullanır.
- Başarısız continuation, daha önce commit edilmiş result, transcript ve continuation state'i korur.
- `Yeni sohbet` aktif işi iptal/geçersiz kılar; transcript, committed result, continuation state, draft save state ve hazırlanmış meal identity sıfırlanır.
- Successful meal save sonrasında aynı aktif result'ın tekrar kaydedilmesi engellenir; başarısız save retry'si aynı yerel record identity'yi kullanır.

Kullanıcı açısından bunun sonucu, timeout/network gibi retry edilebilir bir hatadan sonra bağlamın kaybolmaması ve eski bir cevabın yeni sohbete sızmamasıdır. Configuration, rate limit, timeout, invalid response ve server error durumları retry eligibility açısından ayrı sunulur.

## Image MealAI

Image modu text Chat V2'den ayrı bir akıştır:

```text
Kamera / galeri
    → yerel JPEG hazırlama
    → multipart POST /ai/meals/interpret-image
    → identity ve amount clarification
    → deterministic POST /ai/meals/resolve
    → review
    → yerel MealRecord
```

Uygulama tek bir fotoğraf seçer veya çeker; kamera iznini kontrol eder ve reddedilirse galeriyi alternatif olarak bırakır. Android'de kesintiye uğramış picker sonucu için recovery yolu vardır. Seçilen local image, metadata/base64 taşımadan JPEG'e dönüştürülür; uzun kenar ve compression politikalarıyla hazırlanır, 8 MiB sınırını aşarsa daha küçük fallback pass denenir. Hazırlanmış geçici dosyalar kullanım sonrasında best-effort temizlenir.

Hazırlanan JPEG `image` ve `locale` alanlarıyla multipart olarak `/ai/meals/interpret-image` endpoint'ine yüklenir. Backend'in canonical candidate, amount ve nutrition sonuçları mobilde doğrulanır. Identity belirsizliğinde yalnızca backend candidate'ı; amount belirsizliğinde pozitif gram veya backend'in trusted stored portion seçeneği `/ai/meals/resolve` yoluyla gönderilir. Tüm item'lar hazır olduğunda kullanıcı review kartından yerel günlüğe kaydedebilir.

Image modu Chat V2 continuation state'i kullanmaz, streaming değildir ve görselden güvenilen otomatik ağırlık tahmini iddiasında bulunmaz. Frozen text/chat accuracy evaluation image accuracy'yi ölçmez.

### Text / image yaşam döngüsü

Pristine text session image moduna geçebilir. Aktif bir text conversation yanlışlıkla mode switch ile kaybedilmesin diye geçiş kilitlenir; `Yeni sohbet` text state'ini sıfırlayıp geçişi yeniden açar. Image modundaki `Yeni giriş`, yalnızca image session/input state'ini temizler ve bunu text chat'e taşımaz.

## Günlük

Günlük tab'ı hydrate edilmiş yerel öğünlerden yalnızca cihazın bugünkü local calendar gününe ait kayıtları seçer ve en yeniden eskiye sıralar. Her kayıt saat, yiyecek, resolved grams ve kalori bilgisiyle gösterilir. Günlük özet calories, protein, carbohydrates ve fat toplamlarını hesaplar; eksik nutrient alanlarını sıfıra çevirmek yerine `En az`/bilinmiyor olarak görünür kılar.

Silme işlemi onay ister, tek write ownership'i kullanır ve başarılı persistence sonrasında listeyi günceller. Uzak senkronizasyon yapılmaz.

## 7 Günlük Beslenme Analizi

Analiz penceresi bugünü ve önceki altı local calendar gününü kapsar. Saat cinsinden kayan bir `7 × 24` pencere değildir. UI günleri bugünden geçmişe sıralar ve kayıt olmayan günleri de yedi günlük görünümde açıkça tutar.

Kaynak yalnızca yerel `MealRecord[]` verisidir. Analiz:

- bugünün kalori/makro özetini;
- yedi gündeki kayıtlı gün, öğün ve yiyecek sayısını;
- kayıt bulunan günler üzerinden nutrition ortalamasını;
- günlük kalori görünümünü;
- her nutrient için bilinen/bilinmeyen item kapsamını gösterir.

Eksik nutrient değerleri `null` semantiğini korur. Sonuçlar `exact`, `partial`, `unavailable` veya `no-data` olarak ele alınır; sağlık hedefi, diyet önerisi veya klinik yorum üretilmez.

## PDF Rapor Dışa Aktarımı

PDF raporu backend'e gönderilmez. Aynı yerel yedi günlük veri önce immutable report modeline kopyalanır; tarih, sayı, missing/partial değer ve portion formatları deterministic biçimde hazırlanır. Rapor kronolojik olarak en eski günden bugüne günlük özet, ortalamalar, veri kapsamı ve ayrıntılı öğünleri içerir.

Model HTML'e render edilir, Expo Print ile A4 portrait PDF üretilir ve Expo Sharing platform share sheet'i açar. Paylaşmadan önce üretilen sonucun en az bir sayfa içermesi, dosyanın var olması ve boyutunun sıfırdan büyük olması doğrulanır. Share işlemi tamamlandıktan sonra geçici PDF best-effort silinir.

Uygulama PDF'i backend'e upload etmez veya cloud storage'a kaydetmez. Her platform ya da share destination için exhaustive doğrulama iddiası yoktur.

## Navigation

Expo Router yüzeyi:

- `/(tabs)`
  - `index`: bugünün günlüğü ve nutrition özeti
  - `analysis`: yedi günlük analiz ve PDF export
  - `ai`: text Chat V2 ve ayrı image MealAI modu
  - `profile`: yalnızca placeholder ekran
- `/search`: backend food search
- `/food/[id]`: canonical food detail, portion/gram hesabı, review ve yerel kayıt

Root layout, tüm route'ları ortak `MealStoreProvider` ile sarar.

## Hata Yönetimi

API katmanı configuration, network, HTTP/backend, invalid response ve MealAI timeout hatalarını birbirinden ayırır. Request cancellation ayrı tutulur. Search, detail ve calculation ekranları eski request sahipliğini generation/fingerprint kontrolleriyle reddeder; yeni input eski işi iptal eder.

MealAI UI, yalnızca güvenli kabul edilen network, timeout, rate-limit, invalid-response ve server failure sınıflarında explicit retry sunar. Retry otomatik queue değildir ve background/offline synchronization yoktur. Storage hydration, write, image acquisition/preparation, PDF generation/validation ve sharing hataları kendi kullanıcı durumlarıyla gösterilir.

## Test ve Doğrulama

Mobile repository'nin statik doğrulama komutları:

```sh
npm run typecheck
npx expo-doctor
```

Bu README tesliminde yeniden çalıştırılan static validation:

| Komut | Sonuç |
| --- | --- |
| `npm run typecheck` | `PASS` |
| `npx expo-doctor` | `PASS — 21/21 checks` |

Final Chat V2 live/manual acceptance ayrıntıları [Phase 15 Chat V2 Acceptance](../case-study-eatbetter-backend/PHASE15_CHAT_ACCEPTANCE.md) belgesinde tutulur. Kaydedilmiş kabul kanıtı şunları içerir:

- nutrition query sonucunun read-only kalması;
- doğrudan MealAI logging;
- eksik miktar continuation'ında food identity/`FoodID` korunması;
- multi-food logging;
- başarısız initial turn ve continuation retry;
- `Yeni sohbet` reset davranışı;
- text/image yaşam döngüsü ve image regression;
- 3–4 turn keyboard/scroll kullanılabilirliği.

Food identity/rephrase live acceptance sonucu **`SKIPPED`** olarak kalır; deterministic production-data fixture bulunmadığı için `PASS` sayılmamıştır.

Kaydedilmiş önceki static acceptance kanıtı:

| Komut | Sonuç |
| --- | --- |
| `npm run typecheck` | `PASS` |
| `npx expo-doctor` | `PASS — 21/21 checks` |

Bu kanıtlar exhaustive cihaz, kamera, Android, tablet veya web QA iddiası değildir. Repository'de dedicated automated mobile test framework bulunmadığından reducer/session davranışlarının önemli bir kısmı static inspection ve bounded manual acceptance ile doğrulanmıştır.

Conversational MealAI accuracy, mobil README kapsamından ayrı olarak frozen labeled corpus üzerinde backend contract seviyesinde değerlendirilir. Methodology ve ölçüm sınırları için [Phase 15 AI Accuracy Evaluation](../case-study-eatbetter-backend/docs/phase15-ai-accuracy-evaluation.md) belgesine bakın.

## Temel Mobile Engineering Kararları

- **Backend, canonical food ve nutrition source of truth'tür.** Mobilin AI veya UI üzerinden trusted nutrition uydurmasını önler.
- **Yerel günlük, backend food truth'ten ayrıdır.** Case-study kapsamında açık bir device-local diary sağlar; cloud/account semantiği ima etmez.
- **Text Chat V2 ve image MealAI ayrı tutulur.** İki akışın provider/input ve continuation contract'ları farklıdır; yanlış state migration engellenir.
- **Transcript ile continuation state ayrıdır.** Kullanıcıya gösterilen konuşma, backend replay contract'ının yanlışlıkla semantik kaynağı olmaz.
- **Başarısız chat turn'leri UI açısından transactional davranır.** Kullanıcının turn'ü korunur; başarısız assistant/result/state kısmen commit edilmez ve aynı snapshot retry edilir.
- **Backend item sırası ve active item authoritative'dir.** Mobil taraf belirsiz multi-food sonucunu yeniden sıralamaz veya sessizce çözmez.
- **Analiz ve PDF generation yerel ve deterministic'tir.** Aynı meal snapshot'ı network upload'ı olmadan rapor modeline dönüşür.

## Trade-off'lar ve Bilinen Sınırlamalar

- Meal kayıtları local-only'dir; account, cloud backup veya multi-device sync yoktur.
- Chat transcript'i durably persist edilmez ve server-side conversation history değildir.
- Chat yanıtları streaming değildir.
- Text Chat V2 ile image MealAI farklı backend contract'ları kullanır.
- Dedicated automated mobile test framework yoktur.
- Profil/account özelliği uygulanmamıştır; ilgili tab placeholder'dır.
- Backend kaynaklı `cup`, `tbsp` veya `whipped` gibi bazı portion açıklamaları ayrıca lokalize edilmez.
- AI akışları erişilebilir backend ve ilgili backend provider configuration'ına bağlıdır.
- Frozen accuracy evaluation text/chat contract'ını ölçer; image accuracy benchmark'ı değildir.
- Android, web, tablet ve tüm form factor'lar için exhaustive acceptance yapılmamıştır.

## Sonraki İyileştirmeler

1. Chat retry/concurrency, storage hydration/write, navigation ve PDF üretimi için automated mobile integration/E2E coverage eklemek.
2. Ürün kapsamı multi-device kullanıma genişlerse authentication ile account-backed meal synchronization ve durable history tasarlamak.
3. Accessibility, loading/error durumları, kamera izinleri ve desteklenen cihaz/platform matrisi için daha geniş production UX validation yürütmek.

## Proje Yapısı

```text
app/
├── _layout.tsx                  Root stack + MealStoreProvider
├── (tabs)/
│   ├── index.tsx                Günlük
│   ├── analysis.tsx             7 günlük analiz + PDF export
│   ├── ai.tsx                   Text ve image MealAI yüzeyi
│   └── profile.tsx              Placeholder
├── search.tsx                   Food search
└── food/[id].tsx                Food detail + hesaplama + kayıt

src/
├── api/
│   ├── client.ts                HTTP transport ve ApiError sınıflandırması
│   ├── foods.ts                 Search/detail contract'ları
│   ├── nutrition.ts             Deterministic calculation client'ı
│   └── mealAi.ts                Chat, image ve resolve contract'ları
├── domain/                      Mobile domain modelleri ve aggregation
├── storage/mealStorage.ts       AsyncStorage adapter'ı
├── state/MealStoreProvider.tsx  Hydration ve meal write ownership'i
└── features/
    ├── home/                    Günlük özet ve meal satırları
    ├── food/                    Portion ve nutrition review UI
    ├── mealAi/
    │   ├── MealAiChatPanel.tsx
    │   ├── MealAiReviewCard.tsx
    │   ├── mealAiChatSession.ts
    │   └── useMealAiChatSession.ts
    ├── analysis/                Yedi günlük analysis kartları
    └── report/
        ├── buildSevenDayNutritionReport.ts
        ├── renderNutritionReportHtml.ts
        └── exportNutritionReportPdf.ts
```
