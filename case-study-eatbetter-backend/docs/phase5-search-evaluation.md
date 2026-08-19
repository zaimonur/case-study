# Phase 5 real-catalog search evaluation

Measured on 2026-08-19 against the existing April 2026 USDA database after loading the current committed localization artifact:

- 473,999 canonical foods, including 459,395 branded foods
- 1,895 current Turkish localizations from ruleset `tr-usda-v3`
- 42 deterministic queries: 10 Turkish, 5 ASCII-Turkish, 10 English, 5 misspellings, 5 brands, 3 expected misses, and 4 broad/ambiguous probes
- family expectations use normalized whole tokens in canonical name, resolved display name, or brand; there is no LLM judgment

The artifact was loaded with:

```sh
DATABASE_URL='postgres://eatbetter:eatbetter@localhost:5432/eatbetter?sslmode=disable' \
  go run ./cmd/usda-localize load \
  --artifact data/localizations/usda/2026-04-30/tr.jsonl \
  --manifest data/localizations/usda/2026-04-30/tr.manifest.json
```

Both the total `tr` localization count and the canonical-name freshness join returned 1,895 rows.

Three warmed evaluation repetitions produced:

| Metric | Result |
| --- | ---: |
| Expected family cases | 35 |
| Top-1 family hits | 30 / 35 (85.7%) |
| Top-5 family recall | 32 / 35 (91.4%) |
| Expected no-result correctness | 3 / 3 (100%) |
| End-to-end latency p50 | 57.7 ms |
| End-to-end latency p95 | 545.9 ms |

The exact → whole-word → prefix → fuzzy hierarchy fixed the prior `süt`/`sut` failure. Both queries now return localized canonical milk foods in the first five positions rather than folded-prefix `SUTTER...` products. Turkish `yumurta`, `süt`, `ekmek`, `pirinç`, `peynir`, `brokoli`, `buğday`, `elma`, and `yoğurt`; ASCII-Turkish `sut`, `cig`, `bugday`, and `pirinc`; all ten English probes; and all five real brand probes met their Top-5 family expectation.

The three remaining Top-5 misses are reported rather than hidden:

- `tavuk` returns no result because the current deterministic localization artifact supplies no applicable chicken search signal.
- `millk` favors complete/prefix-like `General Mills...` lexical signals rather than milk.
- `bred` finds an actual catalog misspelling containing the complete token `BRED`; expected bread-family candidates remain outside Top 5.

`brocoli` and `yougurt` reach the expected family inside Top 5 but not at Top 1. Phase 5 deliberately retrieves deterministic lexical candidates and does not perform semantic intent selection.

`EXPLAIN (ANALYZE, BUFFERS)` on the same database showed Phase 5 expression indexes and no sequential scan of the 473,999-row `foods` table:

| Representative stage | Query | Execution time | Phase 5 index nodes | Observation |
| --- | --- | ---: | ---: | --- |
| Exact canonical | `milk` | 5.1 ms | 6 | canonical primary/folded B-tree plus indexed supporting surfaces |
| Whole-word primary/folded | `süt` / `sut` | 63.2 ms | 12 | token-boundary `LIKE` probes over existing trigram GIN indexes |
| Prefix | `brok` | 2.9 ms | 6 | primary/folded trigram GIN bitmap probes |
| Fuzzy | `brocoli` | 263.0 ms | 4 | word-similarity trigram GIN bitmap probes |
| Real brand | `meijer` | 14.6 ms | 6 | indexed canonical and brand probes |

The planner still selects one or two sequential scans for genuinely small localization/alias relations. The plan assertion rejects any sequential scan of `foods`; none of the five normal paths performed a full catalog scan. Whole-word retrieval reuses the existing normalization functions and trigram indexes, so no new migration, denormalized search mirror, or broad index set was added. Fuzzy and broad ambiguous queries remain the principal latency tradeoff, and no cache was introduced.
