# Phase 15 Chat V2 — Task 4 Acceptance

Recorded on 2026-08-24. `PASS`, `SKIPPED`, and `NOT RUN` are used literally; no live or manual result is inferred from deterministic tests.

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

The command was invoked against the default `http://localhost:8080` with a five-second per-request timeout. The local API was unavailable and all attempted HTTP calls returned `transport_failure`. The live acceptance cases are therefore `NOT RUN`, not product `FAIL`.

| Case | Status | Required live expectation |
| --- | --- | --- |
| A — direct nutrition query | NOT RUN | `nutrition_query`, READY, `nutrition_answer`, target resolved to 150 g |
| B — direct meal logging | NOT RUN | `meal_logging`, READY, `meal_ready`, target resolved to 150 g |
| C — two-turn identity regression | NOT RUN | Amount clarification, exact returned state replay, READY at 150 g with unchanged FoodID |
| D — food identity/rephrase | SKIPPED | No production-data phrase is currently declared stable enough for a safe live identity/rephrase assertion; deterministic tests cover constrained candidates and zero-candidate rephrase |
| E — multi-food | NOT RUN | Multiple items in source order; READY or first-unresolved clarification is accepted |
| F — irrelevant/unknown | NOT RUN | `unknown`, EMPTY, `guidance`, zero items |

## Manual mobile acceptance checklist

No simulator or physical-device scenario below was performed during this pass. Every row remains `NOT RUN` until a person executes it and records the observed result.

| Scenario | Status | Concrete checks |
| --- | --- | --- |
| 1. Nutrition query: `150 g az yağlı krem peynir kaç kalori?` | NOT RUN | One user bubble; backend assistant answer; trusted nutrition result; no save action |
| 2. Direct logging: `150 g az yağlı krem peynir yedim.` | NOT RUN | READY review; `Günlüğe Ekle`; save succeeds once; transcript stays visible; record appears in Günlük |
| 3. Missing amount, then `150 g` | NOT RUN | Amount clarification; free-text continuation; identity remains Az yağlı krem peynir; READY with same FoodID |
| 4. Identity/rephrase | NOT RUN | Free text remains available; shortcut creates a normal user bubble; no legacy `/resolve` UI behavior; zero-candidate reply works |
| 5. Multi-food | NOT RUN | Multiple items shown; shortcuts only on active unresolved item; backend determines clarification order |
| 6. Failed initial turn/retry | NOT RUN | Stop backend, send once, restart, Retry; one user bubble; no failed assistant bubble; one successful assistant response |
| 7. Failed continuation/retry | NOT RUN | Commit clarification, stop backend, send `150 g`, restart, Retry; prior chat stays; reply appears once; original continuation state is reused |
| 8. New chat | NOT RUN | Transcript, committed result, and continuation state clear; next message is an initial turn |
| 9. Text/image lifecycle | NOT RUN | Pristine text can switch to photo; active text cannot; `Yeni sohbet` re-enables switching; image `Yeni giriş` clears image state and remains in image mode |
| 10. Image regression | NOT RUN | Gallery; camera where available; preparation; `/interpret-image`; clarification; retry; READY review; persistence; never migrates to `/chat` |
| 11. Keyboard/scroll | NOT RUN | In 3–4 turns, latest content and loading remain visible/reachable; composer works with keyboard open; no competing vertical scroll makes chat unusable |

## Mobile static validation

| Command | Status |
| --- | --- |
| `npm run typecheck` | PASS |
| `npx expo-doctor` | PASS — 21/21 checks |

Static inspection found the intended mode lifecycle guards: active text marks the session non-pristine, `Yeni sohbet` restores pristine state, and image `Yeni giriş` does not assign text mode. This is not a manual PASS; scenario 9 still requires simulator/device reproduction.

## Known limitations intentionally left

- The local live API/provider/data environment was unavailable, so live behavior is not accepted yet.
- Food-identity/rephrase remains optional in the live tool until a stable production-data prerequisite is declared; resolver behavior was not distorted to create one.
- All simulator/device checks, including camera and persistence, remain `NOT RUN`.
- Raw stored-portion measure text such as `cup`, `tbsp`, or `whipped` was not changed; no functional ambiguity was reproduced.
- No chat persistence, streaming, retrieval/RAG, evaluation dataset, accuracy metric, mobile test framework, or broad cleanup was added.
