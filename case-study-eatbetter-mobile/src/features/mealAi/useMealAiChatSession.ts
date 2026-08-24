import { useCallback, useEffect, useReducer, useRef } from 'react';

import { isAbortError } from '../../api/client';
import { sendMealAiChatMessage } from '../../api/mealAi';
import {
  initialMealAiChatSessionState,
  mealAiChatSessionReducer,
  prepareMealAiChatTurn,
} from './mealAiChatSession';
import type {
  MealAiChatSessionAction,
  MealAiChatSessionState,
  MealAiChatTurnSnapshot,
} from './mealAiChatSession';

type ChatRequestOwnership = {
  sessionGeneration: number;
  requestToken: number;
  turnId: number;
  controller: AbortController;
};

export type UseMealAiChatSessionResult = {
  state: MealAiChatSessionState;
  sendMessage: (message: string, locale: string) => Promise<void>;
  retryFailedTurn: () => Promise<void>;
  reset: () => void;
};

function toError(value: unknown): Error {
  return value instanceof Error
    ? value
    : new Error('The MealAI chat operation failed with an unknown error.', { cause: value });
}

export function useMealAiChatSession(): UseMealAiChatSessionResult {
  const [state, dispatch] = useReducer(
    mealAiChatSessionReducer,
    initialMealAiChatSessionState,
  );
  const stateRef = useRef<MealAiChatSessionState>(initialMealAiChatSessionState);
  const lifecycleActiveRef = useRef(true);
  const sessionGenerationRef = useRef(0);
  const nextRequestTokenRef = useRef(0);
  const nextTurnIdRef = useRef(0);
  const activeRequestRef = useRef<ChatRequestOwnership | null>(null);

  const transition = useCallback((action: MealAiChatSessionAction): void => {
    if (!lifecycleActiveRef.current) {
      return;
    }
    stateRef.current = mealAiChatSessionReducer(stateRef.current, action);
    dispatch(action);
  }, []);

  const ownsRequest = useCallback((ownership: ChatRequestOwnership): boolean => {
    const activeOwnership = activeRequestRef.current;
    const runtime = stateRef.current.turnRuntime;
    return (
      lifecycleActiveRef.current &&
      sessionGenerationRef.current === ownership.sessionGeneration &&
      activeOwnership?.sessionGeneration === ownership.sessionGeneration &&
      activeOwnership.requestToken === ownership.requestToken &&
      activeOwnership.turnId === ownership.turnId &&
      runtime.status === 'pending' &&
      runtime.turnId === ownership.turnId
    );
  }, []);

  const clearOwnership = useCallback((ownership: ChatRequestOwnership): void => {
    const activeOwnership = activeRequestRef.current;
    if (
      activeOwnership?.sessionGeneration === ownership.sessionGeneration &&
      activeOwnership.requestToken === ownership.requestToken &&
      activeOwnership.turnId === ownership.turnId
    ) {
      activeRequestRef.current = null;
    }
  }, []);

  const executeTurn = useCallback(
    async (turn: MealAiChatTurnSnapshot, isRetry: boolean): Promise<void> => {
      if (!lifecycleActiveRef.current || activeRequestRef.current !== null) {
        throw new Error('The MealAI chat controller cannot start another request.');
      }

      const ownership: ChatRequestOwnership = {
        sessionGeneration: sessionGenerationRef.current,
        requestToken: ++nextRequestTokenRef.current,
        turnId: turn.turnId,
        controller: new AbortController(),
      };
      activeRequestRef.current = ownership;
      transition(
        isRetry
          ? { type: 'TURN_RETRY_STARTED', turnId: turn.turnId }
          : { type: 'TURN_STARTED', turn },
      );

      try {
        const result = await sendMealAiChatMessage(
          {
            message: turn.message,
            locale: turn.locale,
            state: turn.baseNextState,
          },
          ownership.controller.signal,
        );
        if (!ownsRequest(ownership)) {
          return;
        }

        transition({
          type: 'TURN_SUCCEEDED',
          turnId: turn.turnId,
          result,
          assistantMessageId: `chat-assistant-${turn.turnId}`,
        });
      } catch (error) {
        if (!ownsRequest(ownership)) {
          return;
        }
        if (!isAbortError(error)) {
          transition({ type: 'TURN_FAILED', turnId: turn.turnId, error: toError(error) });
        }
      } finally {
        clearOwnership(ownership);
      }
    },
    [clearOwnership, ownsRequest, transition],
  );

  const sendMessage = useCallback(
    async (message: string, locale: string): Promise<void> => {
      if (!lifecycleActiveRef.current) {
        throw new Error('The MealAI chat controller is no longer active.');
      }
      const turnId = ++nextTurnIdRef.current;
      const turn = prepareMealAiChatTurn(
        stateRef.current,
        message,
        locale,
        turnId,
        `chat-user-${turnId}`,
      );
      await executeTurn(turn, false);
    },
    [executeTurn],
  );

  const retryFailedTurn = useCallback(async (): Promise<void> => {
    if (!lifecycleActiveRef.current) {
      throw new Error('The MealAI chat controller is no longer active.');
    }
    const runtime = stateRef.current.turnRuntime;
    if (runtime.status !== 'failed') {
      throw new Error('There is no failed MealAI chat turn to retry.');
    }
    const turn: MealAiChatTurnSnapshot = {
      turnId: runtime.turnId,
      userMessageId: runtime.userMessageId,
      message: runtime.message,
      locale: runtime.locale,
      baseNextState: runtime.baseNextState,
    };
    await executeTurn(turn, true);
  }, [executeTurn]);

  const reset = useCallback((): void => {
    activeRequestRef.current?.controller.abort();
    activeRequestRef.current = null;
    sessionGenerationRef.current += 1;
    transition({ type: 'RESET' });
  }, [transition]);

  useEffect(() => {
    lifecycleActiveRef.current = true;
    return () => {
      lifecycleActiveRef.current = false;
      activeRequestRef.current?.controller.abort();
      activeRequestRef.current = null;
      sessionGenerationRef.current += 1;
    };
  }, []);

  return { state, sendMessage, retryFailedTurn, reset };
}
