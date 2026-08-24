# Phase 15 MealAI chat accuracy evaluation specification

## Status and scope

This is the pre-run metric contract for the fixed `mealai-chat-v1` answer key in `data/evaluation/mealai-chat-v1.jsonl`. Task 6A defines labels and methodology only. It does not contain measured results and does not authorize a MealAI chat or external-provider run.

The dataset evaluates the conversational pipeline above lexical retrieval: purpose interpretation, canonical identity resolution, amount resolution, clarification behavior, and the trusted materialized outcome. It does not replace or duplicate `cmd/food-search-eval`, whose scope remains deterministic lexical retrieval.

The dataset contains 30 cases. Its primary category counts are:

| Category | Cases |
| --- | ---: |
| `direct_auto_resolvable` | 7 |
| `amount_clarification` | 5 |
| `food_identity_ambiguity` | 5 |
| `identity_specificity` | 5 |
| `multi_food` | 3 |
| `noise_typo_language` | 3 |
| `unknown_non_food` | 2 |

Four cases are multi-turn. Categories are mutually exclusive primary groupings; tags provide overlapping slices such as language, purpose, amount evidence, identity specificity, ambiguity safety, multi-food behavior, and source-order behavior.

## Dataset schema

Each non-empty JSONL line is one case with this exact logical schema. Fields marked optional must be omitted when they are not applicable.

```text
case {
  id: string                         required, unique, non-empty
  category: enum                     required
  tags: string[]                     required, non-empty, unique within the case
  locale: string                     required, valid supported locale syntax
  turns: turn[]                      required, non-empty
  notes: string                      required, non-empty human rationale
}

turn {
  message: string                    required, non-empty
  expect: expectation                required
}

expectation {
  purpose: enum                      required: meal_logging | nutrition_query | unknown
  state: enum                        required: ready | clarification_required | empty
  clarification_kind: enum           required: none | amount | food_identity
  active_item_index: integer         optional; required only for clarification_required
  must_not_auto_resolve: boolean     required, turn-level safety label
  items: expected_item[]             required; may be empty only for an empty outcome
}

expected_item {
  source_order: integer              required; equals the zero-based array index
  expected_food_id: positive integer optional; authoritative canonical comparison key
  allowed_food_ids: integer[]        optional alternative to expected_food_id; sparse use only
  expected_canonical_name: string    optional human-readable diagnostic
  expected_source: string            optional; paired with expected_external_id
  expected_external_id: string       optional; paired with expected_source
  expected_resolved_grams: number    optional; finite and positive
}
```

`items` is always written in expected source order. `source_order` makes accidental reordering detectable without changing FoodID into a fuzzy text assertion. A resolved item normally has one `expected_food_id`. `allowed_food_ids` is reserved for a genuinely plural acceptable ground truth and must not be used to hide resolution errors. Version 1 currently needs no allowed set.

`clarification_kind` is explicit on every turn. `none` is required for `ready` and `empty`; `amount` or `food_identity` is required for `clarification_required`. Exact assistant prose is outside the answer key.

## Ground-truth policy

### Canonical food identity

Labels are human-selected from authoritative local catalog records, not inferred from conversational MealAI output. `expected_food_id` is the runtime scoring key. `expected_canonical_name` is diagnostic only and must never be fuzzy-matched for correctness. `expected_source` and `expected_external_id` anchor the selected record to the source catalog for later review; they are reproducibility metadata rather than runtime substitutes for FoodID.

Identity-defining words such as fat level, preparation, brand, cut, and composition remain part of the intended identity. Broad milk, rice, chicken, and cheese inputs are intentionally clarification cases because choosing one nutrition-bearing canonical record would add facts absent from the input. The unsupported food-like case requires safe zero-candidate clarification.

The low-fat cream-cheese integrity invariant is fixed as follows:

- direct `150 g az yağlı krem peynir` uses FoodID `461916`;
- missing-amount turn 1 uses FoodID `461916` and requests amount clarification;
- its `150 g` continuation remains FoodID `461916` and becomes ready at 150 grams.

### Amount

`expected_resolved_grams` is present only when the message or preserved conversation contains explicit mass evidence. Grams remain unchanged. Kilograms are deterministically multiplied by 1000. No serving weight, density, volume conversion, or typical portion is guessed. Missing quantity, volume-only evidence, and unsupported units require amount clarification unless a later explicit continuation supplies trusted grams or selects a stored portion. Version 1 deliberately avoids portion labels because no additional portion behavior was needed to justify guessing a serving weight.

## Metric contract

Every ratio must be reported as `numerator / denominator` and as a percentage. Denominators exclude cases classified as infrastructure errors under the policy below. Empty denominators must be reported as not applicable, never as 0% or 100%.

### 1. Canonical resolution accuracy

- Unit: labeled food item.
- Eligible: an item with `expected_food_id`, or a specifically justified non-empty `allowed_food_ids`.
- Correct: the actual trusted materialized FoodID equals `expected_food_id`, or belongs to `allowed_food_ids`.
- Excluded: unknown/non-food cases, pure food-identity clarification items with no resolved ground-truth identity, and infrastructure-error cases.
- Report: `correct canonical items / eligible canonical items` and percentage.

Canonical names, display names, mentions, and substrings are not scoring substitutes for FoodID.

### 2. Amount accuracy

- Unit: labeled food item.
- Eligible: an item with `expected_resolved_grams`.
- Correct: the final trusted `resolved_grams` equals the expected value within an absolute tolerance of `0.000001` gram, used only to absorb floating-point representation noise.
- Excluded: intentionally unresolved amount clarifications and infrastructure-error cases.
- Report: `correct amount items / eligible amount items` and percentage.

No relative or percentage tolerance is permitted.

### 3. Clarification correctness

- Unit: labeled turn.
- Eligible: every dataset turn, because every turn has an explicit expected conversational outcome.
- Correct: all applicable turn-level conversational assertions match: purpose; top-level state; clarification kind; active item index when labeled; expected item count; and first-unresolved/source-order behavior where applicable.
- `ready` is correct when evidence is sufficient, the labeled clarification kind is `none`, and no active item is expected.
- `clarification_required` is correct only with the labeled clarification kind and active item.
- `empty` is correct for labeled unknown/non-food turns with zero items and no active item.
- Exact assistant text is never compared.
- Excluded: infrastructure-error cases.
- Report: `correct outcome turns / eligible outcome turns` and percentage.

An incorrect purpose, state, clarification kind, active item, item count, or first-unresolved position makes that turn incorrect for this metric.

### 4. Unsafe auto-resolution rate

- Unit: a turn with `must_not_auto_resolve: true`.
- Unsafe: the system returns `ready` with a canonical food identity when the answer key requires clarification or empty.
- Safe: returning a non-ready clarification or empty outcome. A safe but wrong clarification kind may fail clarification correctness without becoming an unsafe auto-resolution.
- Excluded: infrastructure-error cases.
- Report: `unsafe auto-resolutions / safety turns` and percentage.
- Direction: lower is better; the ideal is 0%.

`must_not_auto_resolve` is turn-scoped. A first turn can forbid automatic identity selection while a later, genuinely disambiguated turn correctly becomes ready. Missing amount alone does not set this label when identity is already safe.

### 5. End-to-end success rate

- Unit: full labeled case.
- Evaluable: a case with no infrastructure failure on any required turn.
- Pass: every applicable invariant across all turns passes, including purpose, state, item count, item source order, canonical FoodID, resolved grams, clarification kind, active unresolved item, final ready/empty behavior, and cross-turn identity/result preservation.
- Fail: any one required invariant is incorrect.
- Report: `fully passed cases / evaluable cases` and percentage.

The evaluator must not average successful turns into a passing case. A multi-turn case is one indivisible end-to-end unit.

## Infrastructure and provider errors

Transport failures, provider timeout/unavailability, HTTP 429 or `ai_rate_limited`, and invalid local/provider configuration are infrastructure errors, not product-accuracy failures. The evaluator must classify them separately and report:

- `infra_errors`, with sanitized case/turn and error-kind diagnostics;
- `total_cases`;
- `evaluable_cases`;
- `infra_error_cases`.

If any turn in a case ends in an infrastructure error after bounded retries, the whole case is an infrastructure-error case and is excluded from every accuracy denominator. If one or more required cases remain infrastructure failures, the complete run status is `INCOMPLETE`. Metrics from surviving cases may be shown only as explicitly partial diagnostics and must not be published as though the full dataset completed.

Provider-invalid structured output that was successfully received and maps to a product/API error is an accuracy failure unless the failure is demonstrably caused by transport, provider availability, rate limiting, timeout, or configuration. The Task 6B evaluator must make this boundary explicit in code and output.

## Task 6B execution policy

The future live evaluator must:

1. execute cases sequentially by default and preserve turn order within each case;
2. support configurable pacing between provider-backed requests;
3. use a bounded per-request timeout;
4. retry HTTP 429 only a bounded number of times;
5. honor a valid `Retry-After` value when available, subject to an overall bounded retry policy;
6. use bounded backoff for retryable provider unavailability only if Task 6B explicitly defines it;
7. never retry indefinitely;
8. distinguish infrastructure failures from product accuracy failures;
9. start every case with no prior conversation state and pass only that case's returned v2 state between its turns;
10. avoid concurrent execution unless a later, documented mode is intentionally selected.

Task 6B must report enough sanitized metadata to reproduce or interpret a run:

- dataset version (`mealai-chat-v1`);
- dataset case count;
- run timestamp including timezone;
- API origin, without credentials or query secrets;
- locale per case;
- conversation contract version;
- configured model name when safely available;
- total case count;
- evaluable case count;
- infrastructure-error count and case count.

API keys, authorization headers, cookies, full secret-bearing configuration, and other credentials must never be recorded.

## Dataset validation and freeze rules

The dataset-specific Go test beside the JSONL file validates size, unique IDs, locale syntax, non-empty turns/messages, supported enums, positive IDs, finite positive grams, allowed-set uniqueness, safety/state consistency, multi-turn coherence, item ordering, metadata pairing, and the low-fat cream-cheese cross-case invariant.

Labels are frozen before the first Task 6 measured evaluation. Future corrections require an explicit dataset-version change or a documented answer-key correction made without inspecting a measured run to tune passing behavior. Task 6A must not change prompts, resolver policy, search ranking, amount resolution, nutrition calculation, HTTP behavior, localization, aliases, migrations, or mobile behavior.
