import type {
  ImageMealAiIntent,
  ImageMealAiItem,
  MealAiIntent,
  MealAiItem,
  MealAiResolveChoice,
  MealAiResolveResult,
} from '../../domain/mealAi';

export type MealAiSessionSource = 'text' | 'image';

export type MealAiSessionDomainItem = MealAiItem | ImageMealAiItem;

export type MealAiResolveRuntime =
  | {
      status: 'idle';
      error: null;
    }
  | {
      status: 'resolving';
      error: null;
    }
  | {
      status: 'error';
      error: Error;
    };

export type TextMealAiSessionItem = {
  source: 'text';
  item: MealAiItem;
  originalIntent: Readonly<MealAiIntent>;
  resolve: MealAiResolveRuntime;
};

export type ImageMealAiSessionItem = {
  source: 'image';
  item: ImageMealAiItem;
  originalIntent: Readonly<ImageMealAiIntent>;
  resolve: MealAiResolveRuntime;
};

export type MealAiSessionItem = TextMealAiSessionItem | ImageMealAiSessionItem;

export type IdleMealAiSessionState = {
  status: 'idle';
  source: null;
  locale: null;
  items: [];
  error: null;
};

export type InterpretingMealAiSessionState = {
  status: 'interpreting';
  source: MealAiSessionSource;
  locale: null;
  items: [];
  error: null;
};

export type EmptyMealAiSessionState = {
  status: 'empty';
  source: MealAiSessionSource;
  locale: string;
  items: [];
  error: null;
};

export type ActiveMealAiSessionState = {
  status: 'active';
  source: MealAiSessionSource;
  locale: string;
  items: MealAiSessionItem[];
  error: null;
};

export type ErrorMealAiSessionState = {
  status: 'error';
  source: MealAiSessionSource;
  locale: null;
  items: [];
  error: Error;
};

export type MealAiSessionState =
  | IdleMealAiSessionState
  | InterpretingMealAiSessionState
  | EmptyMealAiSessionState
  | ActiveMealAiSessionState
  | ErrorMealAiSessionState;

export type MealAiSessionResolveChoice =
  | {
      kind: 'food_identity';
      foodId: number;
    }
  | {
      kind: 'grams';
      grams: number;
    }
  | {
      kind: 'portion';
      portionId: number;
      quantity: number;
    };

export type PreparedMealAiResolveCommand = {
  itemIndex: number;
  locale: string;
  foodId: number;
  originalIntent: Readonly<MealAiIntent>;
  apiChoice: MealAiResolveChoice;
};

export type MealAiSessionAction =
  | { type: 'INTERPRET_STARTED'; source: MealAiSessionSource }
  | { type: 'INTERPRET_EMPTY'; source: MealAiSessionSource; locale: string }
  | {
      type: 'INTERPRET_SUCCEEDED';
      source: MealAiSessionSource;
      locale: string;
      items: MealAiSessionDomainItem[];
    }
  | { type: 'INTERPRET_FAILED'; source: MealAiSessionSource; error: Error }
  | { type: 'RESOLVE_STARTED'; itemIndex: number }
  | { type: 'RESOLVE_SUCCEEDED'; itemIndex: number; item: MealAiSessionDomainItem }
  | { type: 'RESOLVE_FAILED'; itemIndex: number; error: Error }
  | { type: 'RESET' };

const idleResolveRuntime: MealAiResolveRuntime = Object.freeze({
  status: 'idle',
  error: null,
});

export const initialMealAiSessionState: MealAiSessionState = {
  status: 'idle',
  source: null,
  locale: null,
  items: [],
  error: null,
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hasExactlyKeys(value: Record<string, unknown>, keys: string[]): boolean {
  const actualKeys = Object.keys(value);
  return actualKeys.length === keys.length && keys.every((key) => actualKeys.includes(key));
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

function createSessionItem(
  source: MealAiSessionSource,
  item: MealAiSessionDomainItem,
): MealAiSessionItem {
  if (source === 'text' && 'mention' in item) {
    const originalIntent: Readonly<MealAiIntent> = Object.freeze({
      query: item.intent.query,
      quantity: item.intent.quantity,
      unitHint: item.intent.unitHint,
    });
    return {
      source,
      item: { ...item, intent: originalIntent },
      originalIntent,
      resolve: idleResolveRuntime,
    };
  }

  if (source === 'image' && 'observation' in item) {
    const originalIntent: Readonly<ImageMealAiIntent> = Object.freeze({
      query: item.intent.query,
      quantity: null,
      unitHint: null,
    });
    return {
      source,
      item: { ...item, intent: originalIntent },
      originalIntent,
      resolve: idleResolveRuntime,
    };
  }

  throw new Error('The MealAI session source does not match its evidence.');
}

function preserveSessionItemIntent(
  sessionItem: MealAiSessionItem,
  item: MealAiSessionDomainItem,
): MealAiSessionItem {
  if (sessionItem.source === 'text' && 'mention' in item) {
    return {
      ...sessionItem,
      item: { ...item, intent: sessionItem.originalIntent },
      resolve: idleResolveRuntime,
    };
  }

  if (sessionItem.source === 'image' && 'observation' in item) {
    return {
      ...sessionItem,
      item: { ...item, intent: sessionItem.originalIntent },
      resolve: idleResolveRuntime,
    };
  }

  return sessionItem;
}

function updateActiveSessionItem(
  state: MealAiSessionState,
  itemIndex: number,
  update: (item: MealAiSessionItem) => MealAiSessionItem,
): MealAiSessionState {
  if (state.status !== 'active' || itemIndex < 0 || itemIndex >= state.items.length) {
    return state;
  }

  const currentItem = state.items[itemIndex];
  const updatedItem = update(currentItem);
  if (updatedItem === currentItem) {
    return state;
  }

  const items = state.items.map((item, index) => (index === itemIndex ? updatedItem : item));
  return {
    ...state,
    items,
  };
}

export function mealAiSessionReducer(
  state: MealAiSessionState,
  action: MealAiSessionAction,
): MealAiSessionState {
  switch (action.type) {
    case 'INTERPRET_STARTED':
      return {
        status: 'interpreting',
        source: action.source,
        locale: null,
        items: [],
        error: null,
      };
    case 'INTERPRET_EMPTY':
      return {
        status: 'empty',
        source: action.source,
        locale: action.locale,
        items: [],
        error: null,
      };
    case 'INTERPRET_SUCCEEDED':
      return {
        status: 'active',
        source: action.source,
        locale: action.locale,
        items: action.items.map((item) => createSessionItem(action.source, item)),
        error: null,
      };
    case 'INTERPRET_FAILED':
      return {
        status: 'error',
        source: action.source,
        locale: null,
        items: [],
        error: action.error,
      };
    case 'RESOLVE_STARTED':
      return updateActiveSessionItem(state, action.itemIndex, (sessionItem) => ({
        ...sessionItem,
        resolve: {
          status: 'resolving',
          error: null,
        },
      }));
    case 'RESOLVE_SUCCEEDED':
      return updateActiveSessionItem(state, action.itemIndex, (sessionItem) =>
        preserveSessionItemIntent(sessionItem, action.item),
      );
    case 'RESOLVE_FAILED':
      return updateActiveSessionItem(state, action.itemIndex, (sessionItem) => ({
        ...sessionItem,
        resolve: {
          status: 'error',
          error: action.error,
        },
      }));
    case 'RESET':
      return initialMealAiSessionState;
  }
}

export function validateMealAiInterpretCommand(text: unknown, locale: unknown): void {
  if (!isNonBlankString(text) || !isNonBlankString(locale)) {
    throw new Error('Meal interpretation text and locale must not be blank.');
  }
}

export function validateMealAiImageInterpretCommand(image: unknown, locale: unknown): void {
  if (!isRecord(image) || !isNonBlankString(locale)) {
    throw new Error('Meal image interpretation input and locale are invalid.');
  }
}

export function prepareMealAiResolveCommand(
  state: MealAiSessionState,
  itemIndex: number,
  choice: MealAiSessionResolveChoice,
): PreparedMealAiResolveCommand {
  if (state.status !== 'active') {
    throw new Error('A meal item can only be resolved in an active session.');
  }

  if (
    !Number.isSafeInteger(itemIndex) ||
    itemIndex < 0 ||
    itemIndex >= state.items.length
  ) {
    throw new Error('The meal session item index is invalid.');
  }

  const sessionItem = state.items[itemIndex];
  if (sessionItem.item.state === 'ready') {
    throw new Error('The meal session item is already ready.');
  }

  if (sessionItem.resolve.status === 'resolving') {
    throw new Error('The meal session item is already being resolved.');
  }

  if (!isRecord(choice) || typeof choice.kind !== 'string') {
    throw new Error('The meal session resolve choice is invalid.');
  }

  if (choice.kind === 'food_identity') {
    if (
      !hasExactlyKeys(choice, ['kind', 'foodId']) ||
      !isPositiveSafeInteger(choice.foodId) ||
      sessionItem.item.clarification.kind !== 'food_identity'
    ) {
      throw new Error('The food identity choice is invalid for this meal item.');
    }

    const candidate = sessionItem.item.clarification.candidates.find(
      ({ foodId }) => foodId === choice.foodId,
    );
    if (candidate === undefined) {
      throw new Error('The selected food is not a trusted candidate for this meal item.');
    }

    return {
      itemIndex,
      locale: state.locale,
      foodId: candidate.foodId,
      originalIntent: sessionItem.originalIntent,
      apiChoice: { kind: 'food_identity' },
    };
  }

  if (choice.kind === 'grams') {
    if (
      !hasExactlyKeys(choice, ['kind', 'grams']) ||
      !isPositiveFiniteNumber(choice.grams) ||
      sessionItem.item.clarification.kind !== 'amount' ||
      sessionItem.item.clarification.allowDirectGrams !== true ||
      sessionItem.item.food === null
    ) {
      throw new Error('The grams choice is invalid for this meal item.');
    }

    return {
      itemIndex,
      locale: state.locale,
      foodId: sessionItem.item.food.foodId,
      originalIntent: sessionItem.originalIntent,
      apiChoice: {
        kind: 'grams',
        grams: choice.grams,
      },
    };
  }

  if (choice.kind === 'portion') {
    if (
      !hasExactlyKeys(choice, ['kind', 'portionId', 'quantity']) ||
      !isPositiveSafeInteger(choice.portionId) ||
      !isPositiveFiniteNumber(choice.quantity) ||
      sessionItem.item.clarification.kind !== 'amount' ||
      sessionItem.item.food === null
    ) {
      throw new Error('The portion choice is invalid for this meal item.');
    }

    const portion = sessionItem.item.clarification.portions.find(
      ({ portionId }) => portionId === choice.portionId,
    );
    if (portion === undefined) {
      throw new Error('The selected portion is not trusted for this meal item.');
    }

    return {
      itemIndex,
      locale: state.locale,
      foodId: sessionItem.item.food.foodId,
      originalIntent: sessionItem.originalIntent,
      apiChoice: {
        kind: 'portion',
        portionId: portion.portionId,
        quantity: choice.quantity,
      },
    };
  }

  throw new Error('The meal session resolve choice kind is unknown.');
}

export function mealAiIntentsAreExactlyEqual(
  left: Readonly<MealAiIntent>,
  right: Readonly<MealAiIntent>,
): boolean {
  return (
    left.query === right.query &&
    left.quantity === right.quantity &&
    left.unitHint === right.unitHint
  );
}

export function reconstructResolvedMealAiItem(
  sessionItem: MealAiSessionItem,
  choice: MealAiResolveChoice,
  result: MealAiResolveResult,
): MealAiSessionDomainItem {
  if (!mealAiIntentsAreExactlyEqual(sessionItem.originalIntent, result.intent)) {
    throw new Error('The meal continuation changed the original intent.');
  }

  if (choice.kind !== 'food_identity' && result.state !== 'ready') {
    throw new Error('The meal continuation returned an invalid state for the selected choice.');
  }

  if (
    choice.kind === 'grams' &&
    (result.state !== 'ready' ||
      result.selection.kind !== 'grams' ||
      result.selection.grams !== choice.grams)
  ) {
    throw new Error('The meal continuation changed the explicit grams choice.');
  }

  if (
    choice.kind === 'portion' &&
    (result.state !== 'ready' ||
      result.selection.kind !== 'portion' ||
      result.selection.portion.portionId !== choice.portionId ||
      result.selection.portion.quantity !== choice.quantity)
  ) {
    throw new Error('The meal continuation changed the explicit portion choice.');
  }

  if (sessionItem.source === 'text') {
    const mention = sessionItem.item.mention;
    if (result.state === 'ready') {
      return {
        mention,
        intent: sessionItem.originalIntent,
        state: 'ready',
        food: result.food,
        selection: result.selection,
        preview: result.preview,
      };
    }

    return {
      mention,
      intent: sessionItem.originalIntent,
      state: 'clarification_required',
      food: result.food,
      clarification: result.clarification,
    };
  }

  const observation = sessionItem.item.observation;
  if (result.state === 'ready') {
    return {
      observation,
      intent: sessionItem.originalIntent,
      state: 'ready',
      food: result.food,
      selection: result.selection,
      preview: result.preview,
    };
  }

  return {
    observation,
    intent: sessionItem.originalIntent,
    state: 'clarification_required',
    food: result.food,
    clarification: result.clarification,
  };
}

export function isMealAiSessionFullyReady(state: MealAiSessionState): boolean {
  return (
    state.status === 'active' &&
    state.items.length > 0 &&
    state.items.every(({ item }) => item.state === 'ready')
  );
}
