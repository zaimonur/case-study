import type {
  MealAiChatConversationState,
  MealAiChatResult,
} from '../../domain/mealAi';

export type MealAiChatMessage = {
  id: string;
  role: 'user' | 'assistant';
  text: string;
};

type WithoutTransportState<T> = T extends MealAiChatResult
  ? Omit<T, 'assistant' | 'nextState'>
  : never;

export type MealAiChatCommittedResult = WithoutTransportState<MealAiChatResult>;

export type MealAiChatTurnSnapshot = {
  turnId: number;
  userMessageId: string;
  message: string;
  locale: string;
  baseNextState: MealAiChatConversationState | null;
};

export type MealAiChatTurnRuntime =
  | { status: 'idle' }
  | ({ status: 'pending' } & MealAiChatTurnSnapshot)
  | ({ status: 'failed'; error: Error } & MealAiChatTurnSnapshot);

export type MealAiChatSessionState = {
  messages: MealAiChatMessage[];
  committedResult: MealAiChatCommittedResult | null;
  continuationState: MealAiChatConversationState | null;
  turnRuntime: MealAiChatTurnRuntime;
};

export type MealAiChatSessionAction =
  | { type: 'TURN_STARTED'; turn: MealAiChatTurnSnapshot }
  | { type: 'TURN_RETRY_STARTED'; turnId: number }
  | { type: 'TURN_FAILED'; turnId: number; error: Error }
  | { type: 'TURN_SUCCEEDED'; turnId: number; result: MealAiChatResult; assistantMessageId: string }
  | { type: 'RESET' };

export const initialMealAiChatSessionState: MealAiChatSessionState = {
  messages: [],
  committedResult: null,
  continuationState: null,
  turnRuntime: { status: 'idle' },
};

function cloneConversationState(
  state: MealAiChatConversationState,
): MealAiChatConversationState {
  return {
    version: 2,
    purpose: state.purpose,
    activeItemIndex: state.activeItemIndex,
    items: state.items.map((item) => ({
      position: item.position,
      evidence: item.evidence,
      amountEvidence: item.amountEvidence,
      intent: {
        query: item.intent.query,
        quantity: item.intent.quantity,
        unitHint: item.intent.unitHint,
      },
      foodChoiceId: item.foodChoiceId,
      amountChoice:
        item.amountChoice === null
          ? null
          : item.amountChoice.kind === 'grams'
            ? { kind: 'grams', grams: item.amountChoice.grams }
            : {
                kind: 'portion',
                portionId: item.amountChoice.portionId,
                quantity: item.amountChoice.quantity,
              },
    })),
  };
}

export function prepareMealAiChatTurn(
  state: MealAiChatSessionState,
  message: string,
  locale: string,
  turnId: number,
  userMessageId: string,
): MealAiChatTurnSnapshot {
  if (state.turnRuntime.status !== 'idle') {
    throw new Error('A MealAI chat turn is already active.');
  }
  if (message.trim().length === 0 || locale.trim().length === 0) {
    throw new Error('The MealAI chat message and locale must not be blank.');
  }
  if (!Number.isSafeInteger(turnId) || turnId <= 0 || userMessageId.length === 0) {
    throw new Error('The MealAI chat turn identity is invalid.');
  }

  if (state.committedResult === null) {
    if (state.messages.length !== 0 || state.continuationState !== null) {
      throw new Error('The initial MealAI chat state is inconsistent.');
    }
    return { turnId, userMessageId, message, locale, baseNextState: null };
  }

  if (
    state.committedResult.state !== 'clarification_required' ||
    state.continuationState === null
  ) {
    throw new Error('Only a MealAI clarification can accept another turn.');
  }

  return {
    turnId,
    userMessageId,
    message,
    locale,
    baseNextState: cloneConversationState(state.continuationState),
  };
}

function committedResultFromResponse(
  result: MealAiChatResult,
): MealAiChatCommittedResult {
  const { assistant: _assistant, nextState: _nextState, ...committedResult } = result;
  return committedResult;
}

export function mealAiChatSessionReducer(
  state: MealAiChatSessionState,
  action: MealAiChatSessionAction,
): MealAiChatSessionState {
  switch (action.type) {
    case 'TURN_STARTED':
      if (state.turnRuntime.status !== 'idle') {
        return state;
      }
      return {
        ...state,
        messages: [
          ...state.messages,
          { id: action.turn.userMessageId, role: 'user', text: action.turn.message },
        ],
        turnRuntime: { status: 'pending', ...action.turn },
      };
    case 'TURN_RETRY_STARTED':
      if (
        state.turnRuntime.status !== 'failed' ||
        state.turnRuntime.turnId !== action.turnId
      ) {
        return state;
      }
      return {
        ...state,
        turnRuntime: {
          status: 'pending',
          turnId: state.turnRuntime.turnId,
          userMessageId: state.turnRuntime.userMessageId,
          message: state.turnRuntime.message,
          locale: state.turnRuntime.locale,
          baseNextState: state.turnRuntime.baseNextState,
        },
      };
    case 'TURN_FAILED':
      if (
        state.turnRuntime.status !== 'pending' ||
        state.turnRuntime.turnId !== action.turnId
      ) {
        return state;
      }
      return {
        ...state,
        turnRuntime: {
          status: 'failed',
          turnId: state.turnRuntime.turnId,
          userMessageId: state.turnRuntime.userMessageId,
          message: state.turnRuntime.message,
          locale: state.turnRuntime.locale,
          baseNextState: state.turnRuntime.baseNextState,
          error: action.error,
        },
      };
    case 'TURN_SUCCEEDED':
      if (
        state.turnRuntime.status !== 'pending' ||
        state.turnRuntime.turnId !== action.turnId
      ) {
        return state;
      }
      return {
        messages: [
          ...state.messages,
          {
            id: action.assistantMessageId,
            role: 'assistant',
            text: action.result.assistant.text,
          },
        ],
        committedResult: committedResultFromResponse(action.result),
        continuationState:
          action.result.state === 'clarification_required'
            ? cloneConversationState(action.result.nextState)
            : null,
        turnRuntime: { status: 'idle' },
      };
    case 'RESET':
      return initialMealAiChatSessionState;
  }
}

export function isMealAiChatSessionPristine(state: MealAiChatSessionState): boolean {
  return (
    state.messages.length === 0 &&
    state.committedResult === null &&
    state.continuationState === null &&
    state.turnRuntime.status === 'idle'
  );
}
