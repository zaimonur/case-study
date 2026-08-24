# Phase 15 MealAI Accuracy Evaluation

## Executive Summary

The first complete `mealai-chat-v1` baseline evaluated the conversational MealAI path on a frozen, curated set of 30 cases and 34 turns. Fourteen cases passed every applicable invariant and 16 failed, for 14/30 (46.7%) end-to-end success. The weaker canonical, amount, and clarification results are reported without post-run label, evaluator, or product tuning.

| Primary metric | Result | Interpretation |
| --- | ---: | --- |
| Canonical resolution accuracy | 11/31 (35.5%) | Eligible item-level canonical FoodID matches |
| Amount accuracy | 8/24 (33.3%) | Eligible item-level resolved-gram matches |
| Clarification correctness | 16/34 (47.1%) | Turns matching the frozen clarification contract |
| Unsafe auto-resolution rate | 0/7 (0.0%) | Eligible safety turns incorrectly returned as ready; lower is better |
| End-to-end success rate | 14/30 (46.7%) | Cases passing every applicable invariant across all turns |

The dominant case-level symptom was failure to reach the frozen canonical identity: 9 single-food cases and 3 noisy/normalized-input cases ended without the expected FoodID. The report does not retain extracted queries or candidate lists, so it cannot isolate whether each miss originated in interpretation, candidate retrieval, or the deterministic safe-exact resolver gate.

## Evaluation Integrity and Methodology

The answer key was frozen before measured Task 6 runs in Task 6A commit `862ab8d1dcbcc34e7e28a20f2e52265f9b6bae70`. Its 30 cases and 34 turns were then treated as immutable. The first live attempt was `INCOMPLETE` because `tr_almonds_missing_amount_then_grams`, turn 1, exhausted the bounded retry policy with a provider rate-limit error. No accuracy baseline was published from that attempt.

The second and final attempt ran the existing evaluator sequentially against the local API and its configured Groq provider, with fresh state per case, exact `next_state` forwarding, the existing bounded retry and timeout behavior, and a five-second case delay. It completed all 30 cases with zero infrastructure-error cases and was accepted unchanged as the first `COMPLETE` Task 6 baseline. No dataset label, metric contract, evaluator logic, or production MealAI behavior was tuned between attempts or after seeing failures; the complete result was not rerun to improve its appearance.

The baseline was parsed as a single JSON document and checked against the current evaluator report structure. Its frozen identifiers, counts, status, metric values, and empty infrastructure-error set all matched the Task 6 contract.

## Primary Metrics

| Metric | Numerator | Denominator | Percentage |
| --- | ---: | ---: | ---: |
| Canonical resolution accuracy | 11 | 31 eligible items | 35.5% |
| Amount accuracy | 8 | 24 eligible items | 33.3% |
| Clarification correctness | 16 | 34 turns | 47.1% |
| Unsafe auto-resolution rate | 0 | 7 eligible safety turns | 0.0% |
| End-to-end success rate | 14 | 30 cases | 46.7% |

Canonical names and assistant wording were not fuzzy scoring substitutes. Food identity used exact trusted FoodID, amounts used frozen resolved grams, and end-to-end success required every applicable invariant in every turn of a case.

## Category Results

These are primary-category end-to-end results; each case belongs to exactly one row.

| Primary category | Passed | Failed | Total | E2E pass rate |
| --- | ---: | ---: | ---: | ---: |
| `direct_auto_resolvable` | 1 | 6 | 7 | 14.3% |
| `amount_clarification` | 1 | 4 | 5 | 20.0% |
| `food_identity_ambiguity` | 5 | 0 | 5 | 100.0% |
| `identity_specificity` | 4 | 1 | 5 | 80.0% |
| `multi_food` | 0 | 3 | 3 | 0.0% |
| `noise_typo_language` | 1 | 2 | 3 | 33.3% |
| `unknown_non_food` | 2 | 0 | 2 | 100.0% |
| **Total** | **14** | **16** | **30** | **46.7%** |

Selected tag slices are deliberately small, overlapping diagnostic views rather than independent statistical samples.

| Tag slice | Passed | Failed | Total | E2E pass rate |
| --- | ---: | ---: | ---: | ---: |
| `unsafe_resolution_guard` | 7 | 0 | 7 | 100.0% |
| `explicit_grams` | 6 | 9 | 15 | 40.0% |
| `multi_turn` | 2 | 2 | 4 | 50.0% |
| `multi_food` | 0 | 3 | 3 | 0.0% |
| `identity_specificity` | 3 | 6 | 9 | 33.3% |

## Safety

The unsafe auto-resolution rate was 0/7 (0.0%), where lower is better. No eligible turn in the frozen safety slice returned `ready` with a canonical identity when the answer key required clarification or an empty result. This is evidence only for seven curated eligible turns; it is not a general claim that the system is safe across unseen inputs or production traffic. A safe refusal can still be inaccurate and can still fail clarification correctness.

## Failure Analysis

### Assertion failure events

The evaluator recorded 75 assertion failure events across 16 failed cases:

| Assertion failure event | Count |
| --- | ---: |
| `food_id_mismatch` | 18 |
| `clarification_kind_mismatch` | 18 |
| `amount_mismatch` | 14 |
| `active_item_mismatch` | 13 |
| `state_mismatch` | 12 |
| **Total events** | **75** |

These counts are events, not independent failed cases. One unresolved identity can cascade into a wrong clarification kind, wrong active item, absent amount, and wrong top-level state on the same turn. Multi-turn and multi-item cases can add further events while still representing one failed case.

### Primary case-level engineering clusters

Each of the 16 failed cases is assigned to exactly one primary diagnostic cluster below. These are evidence-bounded engineering hypotheses, not proven root causes: the baseline contains materialized outcomes but not the provider's extracted query or the search candidate set.

| Primary engineering cluster | Cases | Count |
| --- | --- | ---: |
| Single-food safe-exact identity resolution gap | `tr_raw_banana_explicit_grams`, `tr_whole_wheat_bread_explicit_grams`, `tr_whole_milk_calorie_query`, `tr_almonds_protein_query`, `tr_boiled_broccoli_explicit_grams`, `tr_whole_milk_missing_amount`, `tr_plain_low_fat_yogurt_missing_amount`, `tr_white_bread_missing_amount`, `tr_raw_whole_egg_specific` | 9 |
| Noisy or normalized-input resolution gap | `tr_almonds_decimal_kg`, `tr_almonds_mild_typo`, `tr_whole_wheat_bread_natural_filler` | 3 |
| Multi-food first-unresolved cascade | `tr_multi_food_all_ready`, `tr_multi_food_second_needs_amount`, `tr_multi_food_first_amount_then_ready` | 3 |
| Continuation blocked by unresolved initial identity | `tr_almonds_missing_amount_then_grams` | 1 |
| **Total failed cases** |  | **16** |

In the first two clusters, the measured output had no expected FoodID and remained in clarification. The existing resolver only auto-resolves one unique safe exact identity, but this artifact cannot determine whether the provider's query, lexical candidate retrieval, or that exact-identity policy was the decisive boundary for a particular case. The multi-food cluster additionally shows how an unresolved earlier item controls the active index and prevents otherwise expected progress.

## Representative Failures

### `tr_raw_banana_explicit_grams`

- **User input:** `100 g çiğ muz yedim.`
- **Expected:** meal logging ready with FoodID `466781` (`Bananas, raw`) and 100 g.
- **Actual:** clarification was required at item 0; FoodID and resolved grams were both absent.
- **Measured events:** `state_mismatch`, `clarification_kind_mismatch`, `active_item_mismatch`, `food_id_mismatch`, `amount_mismatch` (5).
- **Primary cluster and evidence:** single-food safe-exact identity resolution gap; a specific preparation and explicit mass still did not materialize the frozen canonical identity.
- **Next improvement:** inspect and test the extracted Turkish query and bounded candidate evidence for specific preparation aliases while retaining fail-closed identity selection.

### `tr_almonds_decimal_kg`

- **User input:** `0,2 kg badem yedim.`
- **Expected:** meal logging ready with FoodID `463404` (`Nuts, almonds`) and deterministic conversion to 200 g.
- **Actual:** clarification was required at item 0; FoodID and resolved grams were absent.
- **Measured events:** `state_mismatch`, `clarification_kind_mismatch`, `active_item_mismatch`, `food_id_mismatch`, `amount_mismatch` (5).
- **Primary cluster and evidence:** noisy or normalized-input resolution gap; the decimal-comma/kilogram form did not reach a materialized identity or amount. The artifact cannot attribute the miss to amount parsing versus query/retrieval.
- **Next improvement:** add hard-negative and trace-level tests for decimal-comma unit forms through extraction, retrieval, and amount resolution without relaxing exact evidence requirements.

### `tr_almonds_missing_amount_then_grams`

- **Turn 1:** `Badem yedim.` Expected FoodID `463404` with amount clarification. Actual output kept FoodID null and used the wrong clarification kind.
- **Turn 1 → Turn 2:** `30 g` should preserve the almond identity and become ready at 30 g. Actual output still required clarification at item 0 with null FoodID and grams.
- **Measured events:** turn 1: `clarification_kind_mismatch`, `food_id_mismatch`; turn 2: `state_mismatch`, `clarification_kind_mismatch`, `active_item_mismatch`, `food_id_mismatch`, `amount_mismatch` (7 total).
- **Primary cluster and evidence:** continuation blocked by unresolved initial identity; the first turn never established the expected trusted food, so the grams continuation could not complete the labeled amount path.
- **Next improvement:** add continuation regressions that distinguish identity clarification from amount clarification and verify recovery from the exact frozen state.

### `tr_multi_food_second_needs_amount`

- **User input:** `100 g badem ve az yağlı krem peynir yedim.`
- **Expected:** item 0 resolved to almonds at 100 g, item 1 resolved to low-fat cream cheese, and amount clarification active at item 1.
- **Actual:** item 0 had null FoodID and grams, while item 1 reached FoodID `461916`; clarification remained active at item 0.
- **Measured events:** `clarification_kind_mismatch`, `active_item_mismatch`, `food_id_mismatch`, `amount_mismatch` (4).
- **Primary cluster and evidence:** multi-food first-unresolved cascade; the later identity resolved, but the earlier unresolved item changed the expected active item and blocked partial progress.
- **Next improvement:** strengthen ordered multi-item tests and partial-resolution handling while preserving the invariant that uncertainty cannot be skipped.

## What Worked Well

- All 5/5 `food_identity_ambiguity` cases passed end to end, including broad milk, rice, chicken, cheese, and unsupported-food inputs that should not be silently resolved.
- Both 2/2 `unknown_non_food` cases passed, and the complete seven-turn safety slice recorded no unsafe ready auto-resolution.
- The paired low-fat cream-cheese regression passed both direct 150 g input and the missing-amount → 150 g continuation, including identity and amount preservation.

These are bounded observations from the frozen corpus, not broad robustness claims.

## Limitations

- The corpus contains only 30 curated cases and 34 turns.
- Locale coverage is 29 `tr-TR` cases and 1 `en-US` case, so conclusions are primarily Turkish-first.
- Task 6 contains no image-accuracy evaluation.
- It contains no portion, density, or volume-estimation benchmark.
- This is a curated diagnostic corpus, not representative production traffic.
- Category and tag slices are small; tags overlap and are diagnostic rather than statistically independent.
- One complete baseline is a snapshot, not a longitudinal drift study.
- The baseline omits extracted intent queries, candidate lists, and provider wording, limiting root-cause separation at the interpretation/retrieval/resolver boundary.
- No confidence intervals or statistical significance claims are appropriate for this sample.

## Next Accuracy Improvements

1. **Improve candidate recall without weakening trusted identity selection.** The 9 single-food identity cases and 3 noisy-input cases did not reach their frozen FoodIDs → add curated multilingual aliases, typo-tolerant retrieval, hard-negative retrieval tests, and evaluate semantic candidates at the existing search boundary → expected benefit is higher canonical and downstream amount recall → keep deterministic, fail-closed selection and never let the LLM invent FoodID or nutrition truth.
2. **Harden ordered multi-food resolution.** No multi-food case passed (0/3), with one case demonstrating that a later identity could resolve while the first item remained active → add focused multi-item interpretation, candidate, source-order, and partial-clarification regressions → expected benefit is fewer cascaded state/active-item failures and better E2E completion → do not skip unresolved earlier items or reorder source evidence.
3. **Expand continuation coverage and the frozen corpus.** Multi-turn E2E was 2/4, while only 30 curated cases and 34 turns were measured → add labeled continuation paths and a larger independently frozen Turkish-first corpus before comparing a future model or retrieval revision → expected benefit is better diagnosis of state recovery and more credible regression detection → preserve exact `next_state` replay and freeze labels before any measured run.

Semantic retrieval / pgvector was intentionally left as a next-step accuracy improvement rather than added inside the case-study timebox. The current implementation keeps deterministic lexical retrieval as the trusted resolution path and constrains the LLM to semantic interpretation. The architecture retains a clear retrieval extension point so semantic candidate retrieval can later be introduced without making the LLM the source of food identity or nutrition truth. This does not claim that pgvector infrastructure is implemented or ready.

## Reproducibility

- Frozen dataset: [`../data/evaluation/mealai-chat-v1.jsonl`](../data/evaluation/mealai-chat-v1.jsonl)
- Complete baseline: [`../data/evaluation/results/mealai-chat-v1-baseline.json`](../data/evaluation/results/mealai-chat-v1-baseline.json)
- Metric contract: [`phase15-ai-accuracy-evaluation-spec.md`](phase15-ai-accuracy-evaluation-spec.md)
- Evaluator source: [`../cmd/meal-ai-eval/`](../cmd/meal-ai-eval/)
- Task 6A commit: `862ab8d1dcbcc34e7e28a20f2e52265f9b6bae70`
- Frozen dataset SHA-256: `1f01eac070de57bf6bb73cb42e31cf3695642111ec3fc4db1f48080239f5b5d9`
- Baseline artifact commit: `4bb8c77659c5ee809077936500bf91a5b7f4c16f`
- Baseline SHA-256: `ab48b11a5046a09003ec56ba341369f83311b794c359d9de11f8653e24ab7663`
- Baseline status: `COMPLETE`; total/evaluable cases: 30/30; infrastructure-error cases: 0.

The Task 6C analysis was computed offline from the frozen dataset and complete baseline. Recompute both hashes with `shasum -a 256` and use the metric contract for denominator and assertion semantics. Do not rerun the provider merely to reproduce this documentation snapshot.
