import type {
  AmountClarificationMealAiItem,
  ClarificationRequiredMealAiResolveResult,
  FoodIdentityClarificationMealAiItem,
  MealAiAmountClarification,
  MealAiFoodCandidate,
  MealAiFoodIdentityClarification,
  MealAiIntent,
  MealAiItem,
  MealAiNutritionPreview,
  MealAiPortionOption,
  MealAiResolvedFood,
  MealAiResolveChoice,
  MealAiResolveResult,
  MealAiSelection,
  MealInterpretResult,
  ReadyMealAiItem,
  ReadyMealAiResolveResult,
  ResolveMealSelectionInput,
} from '../domain/mealAi';
import type { NutritionValues } from '../domain/nutrition';
import { ApiError, isAbortError, postJson, type ApiJsonResult } from './client';

export type InterpretMealTextInput = {
  text: string;
  locale: string;
};

const MEAL_AI_REQUEST_TIMEOUT_MS = 30_000;

type MealAiAbortSource = 'external' | 'timeout' | null;

function createMealAiAbortError(): Error {
  const error = new Error('The MealAI request was cancelled.');
  error.name = 'AbortError';
  return error;
}

async function postMealAiJson(
  path: string,
  body: unknown,
  externalSignal?: AbortSignal,
): Promise<ApiJsonResult> {
  if (externalSignal?.aborted) {
    throw createMealAiAbortError();
  }

  const internalController = new AbortController();
  const abortOwnership: { source: MealAiAbortSource } = { source: null };

  const claimAbort = (source: Exclude<MealAiAbortSource, null>) => {
    if (abortOwnership.source !== null) {
      return;
    }

    abortOwnership.source = source;
    internalController.abort();
  };

  const handleExternalAbort = () => {
    claimAbort('external');
  };

  externalSignal?.addEventListener('abort', handleExternalAbort, { once: true });
  const timeoutId = setTimeout(() => {
    claimAbort('timeout');
  }, MEAL_AI_REQUEST_TIMEOUT_MS);

  try {
    return await postJson(path, {
      body,
      signal: internalController.signal,
    });
  } catch (error) {
    if (abortOwnership.source === 'timeout') {
      throw new ApiError(
        'timeout',
        'The MealAI request timed out.',
        {},
        { cause: error },
      );
    }

    if (abortOwnership.source === 'external') {
      if (isAbortError(error)) {
        throw error;
      }

      throw createMealAiAbortError();
    }

    throw error;
  } finally {
    clearTimeout(timeoutId);
    externalSignal?.removeEventListener('abort', handleExternalAbort);
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isNonBlankString(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0;
}

function isPositiveSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}

function isPositiveFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0;
}

function isNullableNonNegativeFiniteNumber(value: unknown): value is number | null {
  return value === null || (typeof value === 'number' && Number.isFinite(value) && value >= 0);
}

function hasExactlyKeys(value: Record<string, unknown>, keys: string[]): boolean {
  const actualKeys = Object.keys(value);
  return actualKeys.length === keys.length && keys.every((key) => actualKeys.includes(key));
}

function parseIntent(value: unknown): MealAiIntent | null {
  if (
    !isRecord(value) ||
    !isNonBlankString(value.query) ||
    !(value.quantity === null || isPositiveFiniteNumber(value.quantity)) ||
    !(value.unit_hint === null || isNonBlankString(value.unit_hint))
  ) {
    return null;
  }

  return {
    query: value.query,
    quantity: value.quantity,
    unitHint: value.unit_hint,
  };
}

function parseFood(value: unknown): MealAiResolvedFood | null {
  if (
    !isRecord(value) ||
    !isPositiveSafeInteger(value.food_id) ||
    !isNonBlankString(value.display_name) ||
    !isNonBlankString(value.canonical_name) ||
    !(value.brand === null || isNonBlankString(value.brand))
  ) {
    return null;
  }

  return {
    foodId: value.food_id,
    displayName: value.display_name,
    canonicalName: value.canonical_name,
    brand: value.brand,
  };
}

function parseFoodCandidate(value: unknown): MealAiFoodCandidate | null {
  return parseFood(value);
}

function parsePortionOption(value: unknown): MealAiPortionOption | null {
  if (
    !isRecord(value) ||
    !isPositiveSafeInteger(value.portion_id) ||
    !isPositiveFiniteNumber(value.amount) ||
    !isNonBlankString(value.measure) ||
    !isPositiveFiniteNumber(value.grams)
  ) {
    return null;
  }

  return {
    portionId: value.portion_id,
    amount: value.amount,
    measure: value.measure,
    grams: value.grams,
  };
}

function parseNutrition(value: unknown): NutritionValues | null {
  if (
    !isRecord(value) ||
    !isNullableNonNegativeFiniteNumber(value.calories_kcal) ||
    !isNullableNonNegativeFiniteNumber(value.protein_g) ||
    !isNullableNonNegativeFiniteNumber(value.carbohydrates_g) ||
    !isNullableNonNegativeFiniteNumber(value.fat_g)
  ) {
    return null;
  }

  return {
    caloriesKcal: value.calories_kcal,
    proteinG: value.protein_g,
    carbohydratesG: value.carbohydrates_g,
    fatG: value.fat_g,
  };
}

function parseNutritionPreview(value: unknown): MealAiNutritionPreview | null {
  if (!isRecord(value) || !isPositiveFiniteNumber(value.resolved_grams)) {
    return null;
  }

  const nutrition = parseNutrition(value.nutrition);
  if (nutrition === null) {
    return null;
  }

  return {
    resolvedGrams: value.resolved_grams,
    nutrition,
  };
}

function parseSelection(value: unknown): MealAiSelection | null {
  if (!isRecord(value) || !isPositiveSafeInteger(value.food_id)) {
    return null;
  }

  if (value.kind === 'grams') {
    if (!isPositiveFiniteNumber(value.grams) || value.portion !== null) {
      return null;
    }

    return {
      kind: 'grams',
      foodId: value.food_id,
      grams: value.grams,
    };
  }

  if (value.kind === 'portion') {
    if (value.grams !== null || !isRecord(value.portion)) {
      return null;
    }

    const portion = value.portion;
    if (
      !isPositiveSafeInteger(portion.portion_id) ||
      !isPositiveFiniteNumber(portion.quantity) ||
      !isPositiveFiniteNumber(portion.amount) ||
      !isNonBlankString(portion.measure) ||
      !isPositiveFiniteNumber(portion.portion_grams)
    ) {
      return null;
    }

    return {
      kind: 'portion',
      foodId: value.food_id,
      portion: {
        portionId: portion.portion_id,
        quantity: portion.quantity,
        amount: portion.amount,
        measure: portion.measure,
        portionGrams: portion.portion_grams,
      },
    };
  }

  return null;
}

function parseFoodIdentityClarification(value: unknown): MealAiFoodIdentityClarification | null {
  if (
    !isRecord(value) ||
    value.kind !== 'food_identity' ||
    !isNonBlankString(value.reason) ||
    !Array.isArray(value.candidates) ||
    !Array.isArray(value.portions) ||
    value.portions.length !== 0 ||
    value.allow_direct_grams !== false
  ) {
    return null;
  }

  const candidates: MealAiFoodCandidate[] = [];
  for (const candidateValue of value.candidates) {
    const candidate = parseFoodCandidate(candidateValue);
    if (candidate === null) {
      return null;
    }
    candidates.push(candidate);
  }

  return {
    kind: 'food_identity',
    reason: value.reason,
    candidates,
    portions: [],
    allowDirectGrams: false,
  };
}

function parseAmountClarification(value: unknown): MealAiAmountClarification | null {
  if (
    !isRecord(value) ||
    value.kind !== 'amount' ||
    !isNonBlankString(value.reason) ||
    !Array.isArray(value.candidates) ||
    value.candidates.length !== 0 ||
    !Array.isArray(value.portions) ||
    value.allow_direct_grams !== true
  ) {
    return null;
  }

  const portions: MealAiPortionOption[] = [];
  for (const portionValue of value.portions) {
    const portion = parsePortionOption(portionValue);
    if (portion === null) {
      return null;
    }
    portions.push(portion);
  }

  return {
    kind: 'amount',
    reason: value.reason,
    candidates: [],
    portions,
    allowDirectGrams: true,
  };
}

function parseReadyItem(value: Record<string, unknown>): ReadyMealAiItem | null {
  if (
    value.state !== 'ready' ||
    !isNonBlankString(value.mention) ||
    value.clarification !== null
  ) {
    return null;
  }

  const intent = parseIntent(value.intent);
  const food = parseFood(value.food);
  const selection = parseSelection(value.selection);
  const preview = parseNutritionPreview(value.preview);

  if (
    intent === null ||
    food === null ||
    selection === null ||
    preview === null ||
    selection.foodId !== food.foodId
  ) {
    return null;
  }

  return {
    mention: value.mention,
    intent,
    state: 'ready',
    food,
    selection,
    preview,
  };
}

function parseClarificationItem(
  value: Record<string, unknown>,
): FoodIdentityClarificationMealAiItem | AmountClarificationMealAiItem | null {
  if (
    value.state !== 'clarification_required' ||
    !isNonBlankString(value.mention) ||
    value.selection !== null ||
    value.preview !== null ||
    !isRecord(value.clarification)
  ) {
    return null;
  }

  const intent = parseIntent(value.intent);
  if (intent === null) {
    return null;
  }

  if (value.clarification.kind === 'food_identity') {
    if (value.food !== null) {
      return null;
    }

    const clarification = parseFoodIdentityClarification(value.clarification);
    if (clarification === null) {
      return null;
    }

    return {
      mention: value.mention,
      intent,
      state: 'clarification_required',
      food: null,
      clarification,
    };
  }

  if (value.clarification.kind === 'amount') {
    const food = parseFood(value.food);
    const clarification = parseAmountClarification(value.clarification);
    if (food === null || clarification === null) {
      return null;
    }

    return {
      mention: value.mention,
      intent,
      state: 'clarification_required',
      food,
      clarification,
    };
  }

  return null;
}

function parseItem(value: unknown): MealAiItem | null {
  if (!isRecord(value)) {
    return null;
  }

  if (value.state === 'ready') {
    return parseReadyItem(value);
  }

  if (value.state === 'clarification_required') {
    return parseClarificationItem(value);
  }

  return null;
}

function parseInterpretResult(value: unknown): MealInterpretResult | null {
  if (!isRecord(value) || !Array.isArray(value.items)) {
    return null;
  }

  if (value.state === 'empty') {
    return value.items.length === 0 ? { state: 'empty', items: [] } : null;
  }

  const items: MealAiItem[] = [];
  for (const itemValue of value.items) {
    const item = parseItem(itemValue);
    if (item === null) {
      return null;
    }
    items.push(item);
  }

  if (value.state === 'ready') {
    if (items.length === 0 || items.some((item) => item.state !== 'ready')) {
      return null;
    }
    return { state: 'ready', items: items as ReadyMealAiItem[] };
  }

  if (value.state === 'clarification_required') {
    if (items.length === 0 || !items.some((item) => item.state === 'clarification_required')) {
      return null;
    }
    return { state: 'clarification_required', items };
  }

  return null;
}

function parseResolveResult(value: unknown): MealAiResolveResult | null {
  if (!isRecord(value)) {
    return null;
  }

  const intent = parseIntent(value.intent);
  const food = parseFood(value.food);
  if (intent === null || food === null) {
    return null;
  }

  if (value.state === 'ready') {
    if (value.clarification !== null) {
      return null;
    }

    const selection = parseSelection(value.selection);
    const preview = parseNutritionPreview(value.preview);
    if (selection === null || preview === null || selection.foodId !== food.foodId) {
      return null;
    }

    const result: ReadyMealAiResolveResult = {
      state: 'ready',
      intent,
      food,
      selection,
      preview,
    };
    return result;
  }

  if (value.state === 'clarification_required') {
    if (value.selection !== null || value.preview !== null) {
      return null;
    }

    const clarification = parseAmountClarification(value.clarification);
    if (clarification === null) {
      return null;
    }

    const result: ClarificationRequiredMealAiResolveResult = {
      state: 'clarification_required',
      intent,
      food,
      clarification,
    };
    return result;
  }

  return null;
}

function validateIntentInput(intent: MealAiIntent): void {
  if (
    !isRecord(intent) ||
    !isNonBlankString(intent.query) ||
    !(intent.quantity === null || isPositiveFiniteNumber(intent.quantity)) ||
    !(intent.unitHint === null || isNonBlankString(intent.unitHint))
  ) {
    throw new ApiError('config', 'The meal intent is invalid.');
  }
}

function serializeChoice(choice: MealAiResolveChoice): Record<string, unknown> {
  if (!isRecord(choice) || typeof choice.kind !== 'string') {
    throw new ApiError('config', 'The meal resolution choice is invalid.');
  }

  if (choice.kind === 'food_identity' && hasExactlyKeys(choice, ['kind'])) {
    return { kind: 'food_identity' };
  }

  if (
    choice.kind === 'grams' &&
    hasExactlyKeys(choice, ['kind', 'grams']) &&
    isPositiveFiniteNumber(choice.grams)
  ) {
    return { kind: 'grams', grams: choice.grams };
  }

  if (
    choice.kind === 'portion' &&
    hasExactlyKeys(choice, ['kind', 'portionId', 'quantity']) &&
    isPositiveSafeInteger(choice.portionId) &&
    isPositiveFiniteNumber(choice.quantity)
  ) {
    return {
      kind: 'portion',
      portion_id: choice.portionId,
      quantity: choice.quantity,
    };
  }

  throw new ApiError('config', 'The meal resolution choice is invalid.');
}

export async function interpretMealText(
  input: InterpretMealTextInput,
  signal?: AbortSignal,
): Promise<MealInterpretResult> {
  if (!isRecord(input) || !isNonBlankString(input.text) || !isNonBlankString(input.locale)) {
    throw new ApiError('config', 'The meal interpretation input is invalid.');
  }

  const { data, httpStatus, requestId } = await postMealAiJson(
    '/ai/meals/interpret',
    {
      text: input.text,
      locale: input.locale,
    },
    signal,
  );

  const result = parseInterpretResult(data);
  if (result === null) {
    throw new ApiError('invalid-response', 'The meal interpretation response is invalid.', {
      httpStatus,
      requestId,
    });
  }

  return result;
}

export async function resolveMealSelection(
  input: ResolveMealSelectionInput,
  signal?: AbortSignal,
): Promise<MealAiResolveResult> {
  if (
    !isRecord(input) ||
    !isPositiveSafeInteger(input.foodId) ||
    !isNonBlankString(input.locale)
  ) {
    throw new ApiError('config', 'The meal resolution input is invalid.');
  }

  validateIntentInput(input.intent);
  const choice = serializeChoice(input.choice);

  const { data, httpStatus, requestId } = await postMealAiJson(
    '/ai/meals/resolve',
    {
      food_id: input.foodId,
      locale: input.locale,
      intent: {
        query: input.intent.query,
        quantity: input.intent.quantity,
        unit_hint: input.intent.unitHint,
      },
      choice,
    },
    signal,
  );

  const result = parseResolveResult(data);
  if (result === null || result.food.foodId !== input.foodId) {
    throw new ApiError('invalid-response', 'The meal resolution response is invalid.', {
      httpStatus,
      requestId,
    });
  }

  return result;
}
