import type {
  MealAiIntent,
  MealAiItem,
  MealAiResolveChoice,
  MealAiResolveResult,
} from '../../domain/mealAi';

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

export type MealAiSessionItem = {
  item: MealAiItem;
  originalIntent: Readonly<MealAiIntent>;
  resolve: MealAiResolveRuntime;
};

export type IdleMealAiSessionState = {
  status: 'idle';
  locale: null;
  items: [];
  error: null;
};

export type InterpretingMealAiSessionState = {
  status: 'interpreting';
  locale: null;
  items: [];
  error: null;
};

export type EmptyMealAiSessionState = {
  status: 'empty';
  locale: string;
  items: [];
  error: null;
};

export type ActiveMealAiSessionState = {
  status: 'active';
  locale: string;
  items: MealAiSessionItem[];
  error: null;
};

export type ErrorMealAiSessionState = {
  status: 'error';
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
  | { type: 'INTERPRET_STARTED' }
  | { type: 'INTERPRET_EMPTY'; locale: string }
  | { type: 'INTERPRET_SUCCEEDED'; locale: string; items: MealAiItem[] }
  | { type: 'INTERPRET_FAILED'; error: Error }
  | { type: 'RESOLVE_STARTED'; itemIndex: number }
  | { type: 'RESOLVE_SUCCEEDED'; itemIndex: number; item: MealAiItem }
  | { type: 'RESOLVE_FAILED'; itemIndex: number; error: Error }
  | { type: 'RESET' };

const idleResolveRuntime: MealAiResolveRuntime = Object.freeze({
  status: 'idle',
  error: null,
});

export const initialMealAiSessionState: MealAiSessionState = {
  status: 'idle',
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

function preserveOriginalIntent(
  item: MealAiItem,
  originalIntent: Readonly<MealAiIntent>,
): MealAiItem {
  return {
    ...item,
    intent: originalIntent,
  };
}

function createSessionItem(item: MealAiItem): MealAiSessionItem {
  const originalIntent = Object.freeze({
    query: item.intent.query,
    quantity: item.intent.quantity,
    unitHint: item.intent.unitHint,
  });

  return {
    item: preserveOriginalIntent(item, originalIntent),
    originalIntent,
    resolve: idleResolveRuntime,
  };
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
        locale: null,
        items: [],
        error: null,
      };
    case 'INTERPRET_EMPTY':
      return {
        status: 'empty',
        locale: action.locale,
        items: [],
        error: null,
      };
    case 'INTERPRET_SUCCEEDED':
      return {
        status: 'active',
        locale: action.locale,
        items: action.items.map(createSessionItem),
        error: null,
      };
    case 'INTERPRET_FAILED':
      return {
        status: 'error',
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
      return updateActiveSessionItem(state, action.itemIndex, (sessionItem) => ({
        ...sessionItem,
        item: preserveOriginalIntent(action.item, sessionItem.originalIntent),
        resolve: idleResolveRuntime,
      }));
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
): MealAiItem {
  if (!mealAiIntentsAreExactlyEqual(sessionItem.originalIntent, result.intent)) {
    throw new Error('The meal continuation changed the original intent.');
  }

  if (choice.kind !== 'food_identity' && result.state !== 'ready') {
    throw new Error('The meal continuation returned an invalid state for the selected choice.');
  }

  const mention = sessionItem.item.mention;
  const intent = sessionItem.originalIntent;

  if (result.state === 'ready') {
    return {
      mention,
      intent,
      state: 'ready',
      food: result.food,
      selection: result.selection,
      preview: result.preview,
    };
  }

  return {
    mention,
    intent,
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
