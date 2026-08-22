import { useCallback, useEffect, useReducer, useRef } from 'react';

import { interpretMealText, resolveMealSelection } from '../../api/mealAi';
import { isAbortError } from '../../api/client';
import type { MealAiItem } from '../../domain/mealAi';
import {
  initialMealAiSessionState,
  mealAiSessionReducer,
  prepareMealAiResolveCommand,
  reconstructResolvedMealAiItem,
  validateMealAiInterpretCommand,
} from './mealAiSession';
import type {
  MealAiSessionAction,
  MealAiSessionResolveChoice,
  MealAiSessionState,
  MealAiSessionItem,
} from './mealAiSession';

type InterpretRequestOwnership = {
  sessionGeneration: number;
  requestToken: number;
  controller: AbortController;
};

type ResolveRequestOwnership = {
  sessionGeneration: number;
  requestToken: number;
  controller: AbortController;
};

export type UseMealAiSessionResult = {
  state: MealAiSessionState;
  interpret: (text: string, locale: string) => Promise<void>;
  resolveItem: (itemIndex: number, choice: MealAiSessionResolveChoice) => Promise<void>;
  reset: () => void;
};

function toError(value: unknown): Error {
  if (value instanceof Error) {
    return value;
  }

  return new Error('The MealAI operation failed with an unknown error.', { cause: value });
}

function getSessionItem(state: MealAiSessionState, itemIndex: number): MealAiSessionItem | null {
  if (state.status !== 'active') {
    return null;
  }

  return state.items[itemIndex] ?? null;
}

export function useMealAiSession(): UseMealAiSessionResult {
  const [state, dispatch] = useReducer(mealAiSessionReducer, initialMealAiSessionState);
  const stateRef = useRef<MealAiSessionState>(initialMealAiSessionState);
  const lifecycleActiveRef = useRef(true);
  const sessionGenerationRef = useRef(0);
  const nextRequestTokenRef = useRef(0);
  const activeInterpretRef = useRef<InterpretRequestOwnership | null>(null);
  const activeResolvesRef = useRef(new Map<number, ResolveRequestOwnership>());

  const transition = useCallback((action: MealAiSessionAction) => {
    if (!lifecycleActiveRef.current) {
      return;
    }

    stateRef.current = mealAiSessionReducer(stateRef.current, action);
    dispatch(action);
  }, []);

  const ownsInterpretRequest = useCallback((ownership: InterpretRequestOwnership): boolean => {
    const activeOwnership = activeInterpretRef.current;
    return (
      lifecycleActiveRef.current &&
      sessionGenerationRef.current === ownership.sessionGeneration &&
      activeOwnership?.sessionGeneration === ownership.sessionGeneration &&
      activeOwnership.requestToken === ownership.requestToken
    );
  }, []);

  const ownsResolveRequest = useCallback(
    (itemIndex: number, ownership: ResolveRequestOwnership): boolean => {
      const activeOwnership = activeResolvesRef.current.get(itemIndex);
      return (
        lifecycleActiveRef.current &&
        sessionGenerationRef.current === ownership.sessionGeneration &&
        getSessionItem(stateRef.current, itemIndex) !== null &&
        activeOwnership?.sessionGeneration === ownership.sessionGeneration &&
        activeOwnership.requestToken === ownership.requestToken
      );
    },
    [],
  );

  const clearInterpretOwnership = useCallback((ownership: InterpretRequestOwnership): void => {
    const activeOwnership = activeInterpretRef.current;
    if (
      lifecycleActiveRef.current &&
      sessionGenerationRef.current === ownership.sessionGeneration &&
      activeOwnership?.sessionGeneration === ownership.sessionGeneration &&
      activeOwnership.requestToken === ownership.requestToken
    ) {
      activeInterpretRef.current = null;
    }
  }, []);

  const clearResolveOwnership = useCallback(
    (itemIndex: number, ownership: ResolveRequestOwnership): void => {
      const activeOwnership = activeResolvesRef.current.get(itemIndex);
      if (
        lifecycleActiveRef.current &&
        sessionGenerationRef.current === ownership.sessionGeneration &&
        getSessionItem(stateRef.current, itemIndex) !== null &&
        activeOwnership?.sessionGeneration === ownership.sessionGeneration &&
        activeOwnership.requestToken === ownership.requestToken
      ) {
        activeResolvesRef.current.delete(itemIndex);
      }
    },
    [],
  );

  const abortAndClearOwnedRequests = useCallback((): void => {
    activeInterpretRef.current?.controller.abort();
    for (const ownership of activeResolvesRef.current.values()) {
      ownership.controller.abort();
    }

    activeInterpretRef.current = null;
    activeResolvesRef.current.clear();
  }, []);

  const interpret = useCallback(
    async (text: string, locale: string): Promise<void> => {
      validateMealAiInterpretCommand(text, locale);
      if (!lifecycleActiveRef.current) {
        throw new Error('The MealAI session controller is no longer active.');
      }

      abortAndClearOwnedRequests();
      sessionGenerationRef.current += 1;

      const ownership: InterpretRequestOwnership = {
        sessionGeneration: sessionGenerationRef.current,
        requestToken: ++nextRequestTokenRef.current,
        controller: new AbortController(),
      };
      activeInterpretRef.current = ownership;
      transition({ type: 'INTERPRET_STARTED' });

      try {
        const result = await interpretMealText(
          {
            text,
            locale,
          },
          ownership.controller.signal,
        );

        if (!ownsInterpretRequest(ownership)) {
          return;
        }

        if (result.state === 'empty') {
          transition({ type: 'INTERPRET_EMPTY', locale });
        } else {
          transition({
            type: 'INTERPRET_SUCCEEDED',
            locale,
            items: result.items,
          });
        }
      } catch (error) {
        if (!ownsInterpretRequest(ownership)) {
          return;
        }

        if (!isAbortError(error)) {
          transition({ type: 'INTERPRET_FAILED', error: toError(error) });
        }
      } finally {
        clearInterpretOwnership(ownership);
      }
    },
    [
      abortAndClearOwnedRequests,
      clearInterpretOwnership,
      ownsInterpretRequest,
      transition,
    ],
  );

  const resolveItem = useCallback(
    async (itemIndex: number, choice: MealAiSessionResolveChoice): Promise<void> => {
      if (!lifecycleActiveRef.current) {
        throw new Error('The MealAI session controller is no longer active.');
      }

      const command = prepareMealAiResolveCommand(stateRef.current, itemIndex, choice);

      if (activeResolvesRef.current.has(itemIndex)) {
        throw new Error('The meal session item already has an owned resolve request.');
      }

      const sessionItem = getSessionItem(stateRef.current, itemIndex);
      if (sessionItem === null) {
        throw new Error('The meal session item is no longer active.');
      }

      const ownership: ResolveRequestOwnership = {
        sessionGeneration: sessionGenerationRef.current,
        requestToken: ++nextRequestTokenRef.current,
        controller: new AbortController(),
      };
      activeResolvesRef.current.set(itemIndex, ownership);
      transition({ type: 'RESOLVE_STARTED', itemIndex });

      try {
        const result = await resolveMealSelection(
          {
            foodId: command.foodId,
            locale: command.locale,
            intent: command.originalIntent,
            choice: command.apiChoice,
          },
          ownership.controller.signal,
        );

        if (!ownsResolveRequest(itemIndex, ownership)) {
          return;
        }

        let resolvedItem: MealAiItem;
        try {
          resolvedItem = reconstructResolvedMealAiItem(sessionItem, command.apiChoice, result);
        } catch (error) {
          if (!ownsResolveRequest(itemIndex, ownership)) {
            return;
          }

          transition({ type: 'RESOLVE_FAILED', itemIndex, error: toError(error) });
          return;
        }

        transition({ type: 'RESOLVE_SUCCEEDED', itemIndex, item: resolvedItem });
      } catch (error) {
        if (!ownsResolveRequest(itemIndex, ownership)) {
          return;
        }

        if (!isAbortError(error)) {
          transition({ type: 'RESOLVE_FAILED', itemIndex, error: toError(error) });
        }
      } finally {
        clearResolveOwnership(itemIndex, ownership);
      }
    },
    [clearResolveOwnership, ownsResolveRequest, transition],
  );

  const reset = useCallback((): void => {
    abortAndClearOwnedRequests();
    sessionGenerationRef.current += 1;
    transition({ type: 'RESET' });
  }, [abortAndClearOwnedRequests, transition]);

  useEffect(() => {
    lifecycleActiveRef.current = true;

    return () => {
      lifecycleActiveRef.current = false;
      abortAndClearOwnedRequests();
      sessionGenerationRef.current += 1;
    };
  }, [abortAndClearOwnedRequests]);

  return {
    state,
    interpret,
    resolveItem,
    reset,
  };
}
