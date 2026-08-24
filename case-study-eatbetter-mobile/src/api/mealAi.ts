import { File } from 'expo-file-system';

import type {
  AmountClarificationMealAiItem,
  AmountClarificationImageMealAiItem,
  ClarificationRequiredMealAiResolveResult,
  FoodIdentityClarificationMealAiItem,
  FoodIdentityClarificationImageMealAiItem,
  ImageMealAiItem,
  ImageMealAiIntent,
  ImageMealInterpretResult,
  MealAiAmountClarification,
  MealAiChatAmountChoice,
  MealAiChatAssistant,
  MealAiChatConversationState,
  MealAiChatPurpose,
  MealAiChatResult,
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
  ReadyImageMealAiItem,
  ReadyMealAiItem,
  ReadyMealAiResolveResult,
  ResolveMealSelectionInput,
  SendMealAiChatMessageInput,
} from '../domain/mealAi';
import {
  MEAL_IMAGE_MAX_BYTES,
  type PreparedMealImage,
} from '../domain/mealImage';
import type { NutritionValues } from '../domain/nutrition';
import {
  ApiError,
  isAbortError,
  postFormData,
  postJson,
  type ApiJsonResult,
} from './client';

export type InterpretMealTextInput = {
  text: string;
  locale: string;
};

export type InterpretMealImageInput = {
  image: PreparedMealImage;
  locale: string;
};

const MEAL_AI_REQUEST_TIMEOUT_MS = 30_000;
const MEAL_AI_CHAT_MESSAGE_MAX_CODE_POINTS = 2_000;
const MEAL_AI_CHAT_ASSISTANT_MAX_CODE_POINTS = 1_200;
const MEAL_AI_CHAT_MAX_ITEMS = 12;

type MealAiAbortSource = 'external' | 'timeout' | null;

function createMealAiAbortError(): Error {
  const error = new Error('The MealAI request was cancelled.');
  error.name = 'AbortError';
  return error;
}

async function executeMealAiRequest(
  request: (signal: AbortSignal) => Promise<ApiJsonResult>,
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
    return await request(internalController.signal);
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

function postMealAiJson(
  path: string,
  body: unknown,
  externalSignal?: AbortSignal,
): Promise<ApiJsonResult> {
  return executeMealAiRequest(
    (signal) => postJson(path, { body, signal }),
    externalSignal,
  );
}

function postMealAiFormData(
  path: string,
  body: FormData,
  externalSignal?: AbortSignal,
): Promise<ApiJsonResult> {
  return executeMealAiRequest(
    (signal) => postFormData(path, { body, signal }),
    externalSignal,
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isNonBlankString(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0;
}

function isNormalizedCodePointString(
  value: unknown,
  minimumCodePoints: number,
  maximumCodePoints: number,
): value is string {
  if (typeof value !== 'string' || value.trim() !== value) {
    return false;
  }

  const codePointCount = Array.from(value).length;
  return codePointCount >= minimumCodePoints && codePointCount <= maximumCodePoints;
}

function isPositiveSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}

function isNonNegativeSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
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

function isWellFormedUnicode(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const codeUnit = value.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      const nextCodeUnit = value.charCodeAt(index + 1);
      if (!(nextCodeUnit >= 0xdc00 && nextCodeUnit <= 0xdfff)) {
        return false;
      }
      index += 1;
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
      return false;
    }
  }

  return true;
}

function isBoundedNonBlankUnicodeString(value: unknown, maximumCodePoints: number): value is string {
  return (
    typeof value === 'string' &&
    value.trim().length > 0 &&
    isWellFormedUnicode(value) &&
    Array.from(value).length <= maximumCodePoints
  );
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

function parseImageIntent(value: unknown): ImageMealAiIntent | null {
  if (
    !isRecord(value) ||
    !isNormalizedCodePointString(value.query, 2, 120) ||
    value.quantity !== null ||
    value.unit_hint !== null
  ) {
    return null;
  }

  return {
    query: value.query,
    quantity: null,
    unitHint: null,
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

type ParsedReadyItemFields<TIntent extends MealAiIntent> =
  Omit<ReadyMealAiItem, 'mention' | 'intent'> & { intent: TIntent };

type ParsedClarificationItemFields<TIntent extends MealAiIntent> =
  | (Omit<FoodIdentityClarificationMealAiItem, 'mention' | 'intent'> & { intent: TIntent })
  | (Omit<AmountClarificationMealAiItem, 'mention' | 'intent'> & { intent: TIntent });

function parseReadyItemFields<TIntent extends MealAiIntent>(
  value: Record<string, unknown>,
  intentParser: (value: unknown) => TIntent | null,
): ParsedReadyItemFields<TIntent> | null {
  if (value.state !== 'ready' || value.clarification !== null) {
    return null;
  }

  const intent = intentParser(value.intent);
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
    intent,
    state: 'ready',
    food,
    selection,
    preview,
  };
}

function parseClarificationItemFields<TIntent extends MealAiIntent>(
  value: Record<string, unknown>,
  intentParser: (value: unknown) => TIntent | null,
): ParsedClarificationItemFields<TIntent> | null {
  if (
    value.state !== 'clarification_required' ||
    value.selection !== null ||
    value.preview !== null ||
    !isRecord(value.clarification)
  ) {
    return null;
  }

  const intent = intentParser(value.intent);
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
      intent,
      state: 'clarification_required',
      food,
      clarification,
    };
  }

  return null;
}

function parseReadyItem(value: Record<string, unknown>): ReadyMealAiItem | null {
  if (!isNonBlankString(value.mention)) {
    return null;
  }

  const fields = parseReadyItemFields(value, parseIntent);
  return fields === null ? null : { mention: value.mention, ...fields };
}

function parseClarificationItem(
  value: Record<string, unknown>,
): FoodIdentityClarificationMealAiItem | AmountClarificationMealAiItem | null {
  if (!isNonBlankString(value.mention)) {
    return null;
  }

  const fields = parseClarificationItemFields(value, parseIntent);
  return fields === null ? null : { mention: value.mention, ...fields };
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

function parseReadyImageItem(value: Record<string, unknown>): ReadyImageMealAiItem | null {
  if (!isNormalizedCodePointString(value.observation, 1, 160)) {
    return null;
  }

  const fields = parseReadyItemFields(value, parseImageIntent);
  return fields === null ? null : { observation: value.observation, ...fields };
}

function parseImageClarificationItem(
  value: Record<string, unknown>,
): FoodIdentityClarificationImageMealAiItem | AmountClarificationImageMealAiItem | null {
  if (!isNormalizedCodePointString(value.observation, 1, 160)) {
    return null;
  }

  const fields = parseClarificationItemFields(value, parseImageIntent);
  return fields === null ? null : { observation: value.observation, ...fields };
}

function parseImageItem(value: unknown): ImageMealAiItem | null {
  if (!isRecord(value)) {
    return null;
  }

  if (value.state === 'ready') {
    return parseReadyImageItem(value);
  }

  if (value.state === 'clarification_required') {
    return parseImageClarificationItem(value);
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

function parseImageInterpretResult(value: unknown): ImageMealInterpretResult | null {
  if (!isRecord(value) || !Array.isArray(value.items)) {
    return null;
  }

  if (value.state === 'empty') {
    return value.items.length === 0 ? { state: 'empty', items: [] } : null;
  }

  if (value.items.length === 0 || value.items.length > 12) {
    return null;
  }

  const items: ImageMealAiItem[] = [];
  for (const itemValue of value.items) {
    const item = parseImageItem(itemValue);
    if (item === null) {
      return null;
    }
    items.push(item);
  }

  if (value.state === 'ready') {
    if (items.some((item) => item.state !== 'ready')) {
      return null;
    }
    return { state: 'ready', items: items as ReadyImageMealAiItem[] };
  }

  if (value.state === 'clarification_required') {
    if (!items.some((item) => item.state === 'clarification_required')) {
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

function parseChatPurpose(value: unknown): MealAiChatPurpose | null {
  if (value === 'meal_logging' || value === 'nutrition_query' || value === 'unknown') {
    return value;
  }

  return null;
}

function parseChatAssistant(value: unknown): MealAiChatAssistant | null {
  if (
    !isRecord(value) ||
    !hasExactlyKeys(value, ['kind', 'text']) ||
    !isBoundedNonBlankUnicodeString(value.text, MEAL_AI_CHAT_ASSISTANT_MAX_CODE_POINTS) ||
    !(
      value.kind === 'nutrition_answer' ||
      value.kind === 'meal_ready' ||
      value.kind === 'clarification' ||
      value.kind === 'guidance'
    )
  ) {
    return null;
  }

  return { kind: value.kind, text: value.text };
}

function parseChatAmountChoice(value: unknown): MealAiChatAmountChoice | null {
  if (
    !isRecord(value) ||
    !hasExactlyKeys(value, ['kind', 'grams', 'portion_id', 'quantity'])
  ) {
    return null;
  }

  if (
    value.kind === 'grams' &&
    isPositiveFiniteNumber(value.grams) &&
    value.portion_id === null &&
    value.quantity === null
  ) {
    return { kind: 'grams', grams: value.grams };
  }

  if (
    value.kind === 'portion' &&
    value.grams === null &&
    isPositiveSafeInteger(value.portion_id) &&
    isPositiveFiniteNumber(value.quantity)
  ) {
    return {
      kind: 'portion',
      portionId: value.portion_id,
      quantity: value.quantity,
    };
  }

  return null;
}

function parseChatConversationState(value: unknown): MealAiChatConversationState | null {
  if (
    !isRecord(value) ||
    !hasExactlyKeys(value, ['version', 'purpose', 'items', 'active_item_index']) ||
    value.version !== 2 ||
    !Array.isArray(value.items) ||
    value.items.length > MEAL_AI_CHAT_MAX_ITEMS ||
    !(
      value.active_item_index === null ||
      isNonNegativeSafeInteger(value.active_item_index)
    )
  ) {
    return null;
  }

  const purpose = parseChatPurpose(value.purpose);
  if (purpose === null) {
    return null;
  }

  const items: MealAiChatConversationState['items'] = [];
  for (let index = 0; index < value.items.length; index += 1) {
    const itemValue = value.items[index];
    if (
      !isRecord(itemValue) ||
      !hasExactlyKeys(itemValue, [
        'position',
        'evidence',
        'amount_evidence',
        'intent',
        'food_choice_id',
        'amount_choice',
      ]) ||
      itemValue.position !== index ||
      !isBoundedNonBlankUnicodeString(
        itemValue.evidence,
        MEAL_AI_CHAT_MESSAGE_MAX_CODE_POINTS,
      ) ||
      !(
        itemValue.amount_evidence === null ||
        isBoundedNonBlankUnicodeString(
          itemValue.amount_evidence,
          MEAL_AI_CHAT_MESSAGE_MAX_CODE_POINTS,
        )
      ) ||
      !isRecord(itemValue.intent) ||
      !hasExactlyKeys(itemValue.intent, ['query', 'quantity', 'unit_hint']) ||
      !(
        itemValue.food_choice_id === null ||
        isPositiveSafeInteger(itemValue.food_choice_id)
      )
    ) {
      return null;
    }

    const intent = parseIntent(itemValue.intent);
    const amountChoice =
      itemValue.amount_choice === null
        ? null
        : parseChatAmountChoice(itemValue.amount_choice);
    if (intent === null || (itemValue.amount_choice !== null && amountChoice === null)) {
      return null;
    }

    items.push({
      position: index,
      evidence: itemValue.evidence,
      amountEvidence: itemValue.amount_evidence,
      intent,
      foodChoiceId: itemValue.food_choice_id,
      amountChoice,
    });
  }

  const activeItemIndex = value.active_item_index;
  if (activeItemIndex !== null && activeItemIndex >= items.length) {
    return null;
  }

  return {
    version: 2,
    purpose,
    items,
    activeItemIndex,
  };
}

function chatIntentsAreEqual(left: MealAiIntent, right: MealAiIntent): boolean {
  return (
    left.query === right.query &&
    left.quantity === right.quantity &&
    left.unitHint === right.unitHint
  );
}

function parseChatResult(value: unknown): MealAiChatResult | null {
  if (
    !isRecord(value) ||
    !hasExactlyKeys(value, [
      'purpose',
      'state',
      'assistant',
      'items',
      'active_item_index',
      'next_state',
    ]) ||
    !Array.isArray(value.items) ||
    value.items.length > MEAL_AI_CHAT_MAX_ITEMS ||
    !(
      value.active_item_index === null ||
      isNonNegativeSafeInteger(value.active_item_index)
    )
  ) {
    return null;
  }

  const purpose = parseChatPurpose(value.purpose);
  const assistant = parseChatAssistant(value.assistant);
  const nextState = parseChatConversationState(value.next_state);
  if (purpose === null || assistant === null || nextState === null) {
    return null;
  }

  const items: MealAiItem[] = [];
  for (const itemValue of value.items) {
    if (
      !isRecord(itemValue) ||
      !hasExactlyKeys(itemValue, [
        'mention',
        'intent',
        'state',
        'food',
        'selection',
        'preview',
        'clarification',
      ])
    ) {
      return null;
    }

    const item = parseItem(itemValue);
    if (item === null) {
      return null;
    }
    items.push(item);
  }

  const activeItemIndex = value.active_item_index;
  if (
    nextState.purpose !== purpose ||
    nextState.items.length !== items.length ||
    nextState.activeItemIndex !== activeItemIndex
  ) {
    return null;
  }

  for (let index = 0; index < items.length; index += 1) {
    if (
      nextState.items[index].evidence !== items[index].mention ||
      !chatIntentsAreEqual(nextState.items[index].intent, items[index].intent)
    ) {
      return null;
    }
  }

  if (value.state === 'ready') {
    if (
      purpose === 'unknown' ||
      assistant.kind !== (purpose === 'meal_logging' ? 'meal_ready' : 'nutrition_answer') ||
      items.length === 0 ||
      items.some((item) => item.state !== 'ready') ||
      activeItemIndex !== null
    ) {
      return null;
    }

    return {
      purpose,
      state: 'ready',
      assistant,
      items: items as ReadyMealAiItem[],
      activeItemIndex: null,
      nextState,
    };
  }

  if (value.state === 'clarification_required') {
    if (
      purpose === 'unknown' ||
      assistant.kind !== 'clarification' ||
      activeItemIndex === null ||
      activeItemIndex >= items.length ||
      items[activeItemIndex].state !== 'clarification_required' ||
      items.slice(0, activeItemIndex).some((item) => item.state === 'clarification_required')
    ) {
      return null;
    }

    return {
      purpose,
      state: 'clarification_required',
      assistant,
      items,
      activeItemIndex,
      nextState,
    };
  }

  if (
    value.state === 'empty' &&
    assistant.kind === 'guidance' &&
    items.length === 0 &&
    activeItemIndex === null
  ) {
    return {
      purpose,
      state: 'empty',
      assistant,
      items: [],
      activeItemIndex: null,
      nextState,
    };
  }

  return null;
}

function serializeChatConversationState(
  state: MealAiChatConversationState,
): Record<string, unknown> {
  const items = state.items.map((item) => {
    const amountChoice =
      item.amountChoice === null
        ? null
        : item.amountChoice.kind === 'grams'
          ? {
              kind: 'grams',
              grams: item.amountChoice.grams,
              portion_id: null,
              quantity: null,
            }
          : {
              kind: 'portion',
              grams: null,
              portion_id: item.amountChoice.portionId,
              quantity: item.amountChoice.quantity,
            };

    return {
      position: item.position,
      evidence: item.evidence,
      amount_evidence: item.amountEvidence,
      intent: {
        query: item.intent.query,
        quantity: item.intent.quantity,
        unit_hint: item.intent.unitHint,
      },
      food_choice_id: item.foodChoiceId,
      amount_choice: amountChoice,
    };
  });

  const serialized = {
    version: state.version,
    purpose: state.purpose,
    items,
    active_item_index: state.activeItemIndex,
  };
  if (parseChatConversationState(serialized) === null) {
    throw new ApiError('config', 'The MealAI chat continuation state is invalid.');
  }

  return serialized;
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

function createMealImageFormFile(image: PreparedMealImage): File {
  if (
    !isRecord(image) ||
    !isNonBlankString(image.uri) ||
    image.mimeType !== 'image/jpeg' ||
    !isPositiveSafeInteger(image.sizeBytes) ||
    image.sizeBytes > MEAL_IMAGE_MAX_BYTES ||
    !isPositiveSafeInteger(image.width) ||
    !isPositiveSafeInteger(image.height)
  ) {
    throw new ApiError('config', 'The prepared meal image is invalid.');
  }

  try {
    if (new URL(image.uri).protocol !== 'file:') {
      throw new ApiError('config', 'The prepared meal image is invalid.');
    }

    const file = new File(image.uri);
    const info = file.info();
    if (
      !info.exists ||
      info.size !== image.sizeBytes ||
      file.type !== image.mimeType
    ) {
      throw new ApiError('config', 'The prepared meal image is unavailable.');
    }

    // Expo's native FormData reads File.name; setting one own property keeps
    // the binary local-file-backed while controlling the multipart filename.
    Object.defineProperty(file, 'name', {
      configurable: true,
      value: 'meal-image.jpg',
    });
    return file;
  } catch (error) {
    if (error instanceof ApiError) {
      throw error;
    }

    throw new ApiError(
      'config',
      'The prepared meal image is unavailable.',
      {},
      { cause: error },
    );
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

export async function sendMealAiChatMessage(
  input: SendMealAiChatMessageInput,
  signal?: AbortSignal,
): Promise<MealAiChatResult> {
  if (
    !isRecord(input) ||
    !hasExactlyKeys(input, ['message', 'locale', 'state']) ||
    !isBoundedNonBlankUnicodeString(
      input.message,
      MEAL_AI_CHAT_MESSAGE_MAX_CODE_POINTS,
    ) ||
    !isNonBlankString(input.locale) ||
    !(input.state === null || isRecord(input.state))
  ) {
    throw new ApiError('config', 'The MealAI chat input is invalid.');
  }

  const state =
    input.state === null
      ? null
      : serializeChatConversationState(input.state as MealAiChatConversationState);
  const { data, httpStatus, requestId } = await postMealAiJson(
    '/ai/meals/chat',
    {
      message: input.message,
      locale: input.locale,
      state,
    },
    signal,
  );

  const result = parseChatResult(data);
  if (result === null) {
    throw new ApiError('invalid-response', 'The MealAI chat response is invalid.', {
      httpStatus,
      requestId,
    });
  }

  return result;
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

export async function interpretMealImage(
  input: InterpretMealImageInput,
  signal?: AbortSignal,
): Promise<ImageMealInterpretResult> {
  if (!isRecord(input) || !isNonBlankString(input.locale)) {
    throw new ApiError('config', 'The image meal interpretation input is invalid.');
  }

  const imageFile = createMealImageFormFile(input.image);
  const body = new FormData();
  body.append('image', imageFile);
  body.append('locale', input.locale);

  const { data, httpStatus, requestId } = await postMealAiFormData(
    '/ai/meals/interpret-image',
    body,
    signal,
  );

  const result = parseImageInterpretResult(data);
  if (result === null) {
    throw new ApiError('invalid-response', 'The image meal interpretation response is invalid.', {
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
