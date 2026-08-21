import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';

import type { MealRecord } from '../domain/meal';
import { loadMeals, saveMeals } from '../storage/mealStorage';

type HydrationStatus = 'hydrating' | 'ready' | 'error';

type MealStoreValue = {
  meals: MealRecord[];
  hydrationStatus: HydrationStatus;
  hydrationError: Error | null;
  retryHydration: () => void;
  addMeal: (meal: MealRecord) => Promise<void>;
};

const MealStoreContext = createContext<MealStoreValue | null>(null);

type MealStoreProviderProps = {
  children: React.ReactNode;
};

export function MealStoreProvider({ children }: MealStoreProviderProps) {
  const [meals, setMeals] = useState<MealRecord[]>([]);
  const [hydrationStatus, setHydrationStatus] = useState<HydrationStatus>('hydrating');
  const [hydrationError, setHydrationError] = useState<Error | null>(null);
  const [hydrationAttempt, setHydrationAttempt] = useState(0);
  const mealsRef = useRef<MealRecord[]>([]);
  const hydrationStatusRef = useRef<HydrationStatus>('hydrating');
  const writeInFlightRef = useRef(false);

  const retryHydration = useCallback(() => {
    hydrationStatusRef.current = 'hydrating';
    setHydrationStatus('hydrating');
    setHydrationAttempt((attempt) => attempt + 1);
  }, []);

  const addMeal = useCallback(async (meal: MealRecord) => {
    if (hydrationStatusRef.current !== 'ready') {
      throw new Error('Meals cannot be written before hydration is ready.');
    }

    if (writeInFlightRef.current) {
      throw new Error('Another meal write is already in progress.');
    }

    writeInFlightRef.current = true;

    try {
      const nextMeals = [...mealsRef.current, meal];
      await saveMeals(nextMeals);
      mealsRef.current = nextMeals;
      setMeals(nextMeals);
    } finally {
      writeInFlightRef.current = false;
    }
  }, []);

  useEffect(() => {
    let isActive = true;

    hydrationStatusRef.current = 'hydrating';
    setHydrationStatus('hydrating');
    setHydrationError(null);

    void loadMeals()
      .then((persistedMeals) => {
        if (!isActive) {
          return;
        }

        mealsRef.current = persistedMeals;
        hydrationStatusRef.current = 'ready';
        setMeals(persistedMeals);
        setHydrationStatus('ready');
      })
      .catch((error: unknown) => {
        if (!isActive) {
          return;
        }

        hydrationStatusRef.current = 'error';
        setHydrationError(error instanceof Error ? error : new Error('Meal hydration failed.'));
        setHydrationStatus('error');
      });

    return () => {
      isActive = false;
    };
  }, [hydrationAttempt]);

  const value = useMemo(
    () => ({ meals, hydrationStatus, hydrationError, retryHydration, addMeal }),
    [meals, hydrationStatus, hydrationError, retryHydration, addMeal],
  );

  return <MealStoreContext.Provider value={value}>{children}</MealStoreContext.Provider>;
}

export function useMeals(): MealStoreValue {
  const value = useContext(MealStoreContext);

  if (value === null) {
    throw new Error('useMeals must be used within MealStoreProvider.');
  }

  return value;
}
