# Phase 15 Chat V2 — Task 4 Acceptance

Recorded on 2026-08-24. `PASS` and `SKIPPED` are used literally; no live or manual result is inferred from deterministic tests.

## Automated acceptance

| Evidence | Status | Scope |
| --- | --- | --- |
| `make verify` | PASS | Race-enabled Go tests, vet, API/tool builds, legacy `meal-ai-smoke`, and dedicated `meal-ai-chat-smoke` build |
| `cmd/meal-ai-chat-smoke` deterministic tests | PASS | Required case ordering, exact v2 continuation-state replay, common response contract validation, required-failure exit behavior, bounded/safe reporting |
| `internal/application/mealai` chat tests | PASS | Direct nutrition, food-specificity invariant, amount continuation with unchanged FoodID, multi-food order, unknown input, constrained choices, state/replay rejection |
| `internal/httpapi/mealai_chat_test.go` | PASS | Real `/ai/meals/chat` HTTP mapping, strict request decoding, continuation-state decoding, method/error handling, malformed-result rejection |
| `internal/adapters/groq/chat_test.go` | PASS | Nutrition-free schemas, provider output validation, disallowed FoodID rejection, timeout and cancellation classification |
| `internal/application/mealai/assistant_test.go` | PASS | Assistant prose and kind are derived from materialized state and trusted `NutritionPreview` |
| Failed-continuation state safety regression | PASS | A failure after applying a grams decision returns no partial result and does not mutate the caller's committed v2 state |

The existing `cmd/meal-ai-smoke` remains unchanged. Its cases exercise the legacy `/ai/meals/interpret` and `/ai/meals/resolve` flow: empty/negated/injection inputs, direct grams, count input, mixed meal, food identity continuation, grams continuation, and stored-portion continuation.

The dedicated live command is:

```sh
make ai-chat-smoke
# Custom origin/timeout:
make ai-chat-smoke AI_CHAT_SMOKE_ARGS='-base-url http://localhost:8080 -timeout 20s'
```

It only calls `POST /ai/meals/chat`; it does not persist meals or mutate the database. Every successful response is checked for supported purpose/state, compatible assistant kind, nonblank assistant text, v2 next-state consistency, ordered evidence/intent replay, active-item consistency, first-unresolved behavior, and READY/CLARIFICATION_REQUIRED/EMPTY shape rules.

## Live smoke matrix

The later required live smoke run against the local API is the final Task 4 live acceptance result. An earlier attempt encountered transient provider rate limiting; that infrastructure failure is not counted as a product failure.

| Case | Status | HTTP | Purpose | State | Assistant kind | Items / reason |
| --- | --- | ---: | --- | --- | --- | --- |
| A — `direct_nutrition_query` | PASS | 200 | `nutrition_query` | `ready` | `nutrition_answer` | 1 item |
| B — `direct_meal_logging` | PASS | 200 | `meal_logging` | `ready` | `meal_ready` | 1 item |
| C — `amount_continuation_identity_regression` | PASS | 200 | `meal_logging` | `ready` | `meal_ready` | 1 item |
| D — `food_identity_rephrase` | SKIPPED | — | — | — | — | `prerequisite_unavailable`; no stable deterministic production-data fixture |
| E — `multi_food` | PASS | 200 | `meal_logging` | `clarification_required` | `clarification` | 2 items |
| F — `irrelevant_unknown` | PASS | 200 | `unknown` | `empty` | `guidance` | 0 items |

Summary: 6 total, 5 passed, 0 failed, 1 skipped, and 0 required failures.

## Manual mobile acceptance checklist

The following statuses record the scenarios that were actually verified. They do not imply exhaustive device or camera coverage.

| Scenario | Status | Verified result |
| --- | --- | --- |
| 1. Nutrition query: `150 g az yağlı krem peynir kaç kalori?` | PASS | Trusted nutrition result displayed at 150 g; no meal-save action |
| 2. Direct logging: `150 g az yağlı krem peynir yedim.` | PASS | READY result; meal persisted successfully |
| 3. Missing amount → `150 g` | PASS | Amount clarification and free-text continuation completed; low-fat cream-cheese specificity and the same FoodID were preserved; result became READY at 150 g |
| 4. Food identity/rephrase | SKIPPED | No stable live production-data fixture is declared for a deterministic assertion; tests cover constrained candidate selection, zero-candidate rephrase, and trusted pipeline rerun after rephrase |
| 5. Multi-food | PASS | 150 g low-fat cream cheese and 150 g low-sodium fluid milk both resolved with their amounts and were persisted together |
| 6. Failed initial turn/retry | PASS | Failed initial request remained retryable; retry completed; no duplicate failed assistant output was committed |
| 7. Failed continuation/retry | PASS | Prior conversation remained intact; original continuation state was reused; retry completed without duplicate output |
| 8. New chat | PASS | Transcript, committed result, and continuation state reset; the next text message behaved as a new initial turn |
| 9. Text/image lifecycle | PASS | Pristine text could switch to image; active text prevented accidental switching; New Chat restored availability; image New Input cleared image state without migrating into text chat |
| 10. Image regression | PASS | Existing image MealAI flow remained functional and separate from `/chat`; no exhaustive camera/device claim is made |
| 11. Keyboard/scroll | PASS | A 3–4 turn interaction remained usable; latest content/loading stayed reachable and the composer remained usable with keyboard interaction |

## Mobile static validation

| Command | Status |
| --- | --- |
| `npm run typecheck` | PASS |
| `npx expo-doctor` | PASS — 21/21 checks |

Static inspection separately found the intended mode lifecycle guards: active text marks the session non-pristine, `Yeni sohbet` restores pristine state, and image `Yeni giriş` does not assign text mode. The later manual result for scenario 9 is recorded independently above.

## Task 6 accuracy evaluation

Later Task 6 work added the frozen `mealai-chat-v1` labeled dataset, an automated evaluator, the first `COMPLETE` measured baseline, and an offline failure analysis. This is a separate accuracy/evaluation evidence surface, not another Task 4 acceptance PASS; the complete baseline contains measured product failures. See [`docs/phase15-ai-accuracy-evaluation.md`](docs/phase15-ai-accuracy-evaluation.md).

## Known limitations intentionally left

- Live food-identity/rephrase acceptance remains `SKIPPED` because no stable deterministic production-data fixture is declared; retrieval/resolver behavior was not distorted to manufacture a live PASS.
- Chat history is not persisted as a durable server-side conversation.
- Chat responses are not streamed.
- Semantic retrieval, RAG, embeddings, and pgvector are not implemented.
- No mobile automated test framework was added.
- Raw source-native portion wording such as `cup`, `tbsp`, or `whipped` was not turned into a localization subsystem.
- The Task 6 evaluation corpus is intentionally small; detailed accuracy limitations remain in the dedicated Task 6C document.
