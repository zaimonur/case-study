# Phase 5 real-catalog search evaluation

Measured on 2026-08-19 against the existing April 2026 USDA database:

- 473,999 canonical foods, including 459,395 branded foods
- 339 current Turkish localizations
- 42 deterministic queries: 10 Turkish, 5 ASCII-Turkish, 10 English, 5 misspellings, 5 brands, 3 expected misses, and 4 broad/ambiguous probes
- family expectations use normalized whole tokens in canonical name, resolved display name, or brand; there is no LLM judgment

Three warmed repetitions produced:

| Metric | Result |
| --- | ---: |
| Expected family cases | 35 |
| Top-1 family hits | 25 / 35 (71.4%) |
| Top-5 family recall | 29 / 35 (82.9%) |
| Expected no-result correctness | 3 / 3 (100%) |
| End-to-end latency p50 | 26.9 ms |
| End-to-end latency p95 | 543.2 ms |

Representative successful retrievals included Turkish `yumurta`, `pirinç`, `brokoli`, `buğday`, and `yoğurt`; ASCII-Turkish `cig`, `bugday`, and `pirinc`; all ten English food-family probes; all five real brands; and conservative typo retrieval for `brocoli`, `bred`, `cheze`, and `yougurt` within Top 5.

The six Top-5 misses were `süt`, `sut`, `peynir`, `tavuk`, `elma`, and `millk`. The current 339-row localization artifact has no applicable milk/cheese/chicken/apple search signal for the first five. In particular, folded prefix retrieval for `süt`/`sut` finds canonical `SUTTER...` products; deterministic exact/prefix ordering correctly prevents fuzzy results from outranking them, but the candidates are not semantically milk. `millk` fuzzy retrieval favors `General Mills...`. These are reported as catalog-signal/ranking limitations, not hidden as successes; Phase 5 deliberately performs retrieval rather than semantic intent selection.

`EXPLAIN (ANALYZE, BUFFERS)` on the same database showed Phase 5 indexes and no sequential scan of the 473,999-row `foods` table:

| Representative stage | Query | Execution time | Plan observation |
| --- | --- | ---: | --- |
| Exact | `milk` | 9.6 ms | canonical primary/folded B-tree bitmap probes |
| Prefix | `brok` | 5.7 ms | primary/folded trigram GIN bitmap probes |
| Fuzzy | `brocoli` | 238.4 ms | word-similarity trigram GIN bitmap probes |

The initial exact plan used trigram GIN alone and took about 228 ms because short equality probes caused many index rechecks. Adding only two measured canonical-name B-tree expression indexes reduced that plan to 9.6 ms. Small sequential scans over the 339-row localization table remain planner-selected and bounded by that small table; large catalog scans do not occur. Fuzzy and very broad queries remain the principal latency tradeoff, and no cache or denormalized search mirror was introduced.
