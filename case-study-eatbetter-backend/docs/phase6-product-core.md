# Phase 6 product-core validation

Phase 6 keeps one deterministic invariant: one canonical `foods.id` plus one resolved gram amount produces one persisted-source nutrition result. Food detail and calculation integration tests use a small isolated PostgreSQL fixture; the normal suite never requires the USDA dataset.

## Real-catalog product-policy check

Measured on 2026-08-20 against the existing April 2026 USDA catalog without modifying it:

- 473,999 canonical foods
- 44 evaluation queries after adding explicit product-policy cases
- lexical Top-1 family hits: 32 / 37 (86.5%)
- lexical Top-5 family recall: 34 / 37 (91.9%)
- expected no-result correctness: 3 / 3 (100%)
- strengthened product-policy success: 4 / 4 (100%)
- three-repetition end-to-end latency: p50 124.3 ms, p95 960.5 ms; fuzzy/broad probes remain the tail-latency driver

The strengthened product-policy cases require generic `milk` Top-1 to have both generic identity and milk food-family relevance. Explicit `Kroger milk` and reversed `milk Kroger` require Kroger brand evidence and milk food-family evidence on the same Top-1 candidate; brand-only `meijer` requires Meijer brand evidence. Food relevance inspects only canonical/display text, while brand relevance inspects only persisted brand text. Historical OR-based lexical metrics remain unchanged.

Explicit brand-product intent now falls back to ordinary search with the original full normalized query only when the branded product search returns zero candidates. Branded-search database/internal errors are propagated without fallback. The deterministic fixture covers the false-positive `Apple` brand plus generic `Apple pie` case.

The historical lexical misses remain visible: `tavuk` lacks a current deterministic localization signal; `millk` favors complete General Mills signals; and `bred` finds a real catalog `BRED` token before bread-family results.

## Query plans

`EXPLAIN (ANALYZE, BUFFERS)` used the existing Phase 5 expression indexes and performed no sequential scan of the 473,999-row `foods` table on the materially changed product paths:

| Path | Representative input | Execution time |
| --- | --- | ---: |
| Persisted brand phrase resolution | `Kroger milk` phrase set | 18.0 ms |
| Product phrase inside brand | brand `Kroger`, product `milk` | 44.3 ms |
| Brand-only catalog | `meijer` | 13.7 ms |
| Crowded generic/common recovery | generic `milk` | 335.1 ms |

An initial phrase-resolver plan contained a full `foods` scan in a secondary ordering subquery. The resolver was changed to derive primary-vs-folded evidence from the already indexed match set; the repeated plan then had no full catalog scan. No migration `000006` or new index was needed.

Each ordinary lexical stage remains capped at `max(40, public_limit × 5)`. Exact, whole-word, and prefix widening stops once the pool meets the public limit and contains credible stable-identifier-based generic evidence. If a strong stage fills the public limit but hides all generic/common evidence, one bounded indexed generic-strong query collects exact/whole-word/prefix evidence while excluding GTIN products. Fuzzy remains conditional on an underfilled final pool and a query of at least three characters.

Representative HTTP smoke measurements were 362.0 ms for the crowded real-catalog `milk` query, 9.5 ms for food detail, 5.0 ms for a stored-portion calculation, and 2.6 ms for direct grams. The selected real food returned the same persisted-source nutrition contract through both calculation modes.

Known limitations are deliberate: no semantic food auto-selection, no density or ml-to-gram inference, no parsing of USDA free-form portion text, and no AI-generated nutrition.
