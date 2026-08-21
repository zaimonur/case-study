import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import type { MealRecord } from '../domain/meal';
import { loadMeals } from '../storage/mealStorage';

type HydrationStatus = 'hydrating' | 'ready' | 'error';

type MealStoreValue = {
  meals: MealRecord[];
  hydrationStatus: HydrationStatus;
  hydrationError: Error | null;
  retryHydration: () => void;
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

  const retryHydration = useCallback(() => {
    setHydrationAttempt((attempt) => attempt + 1);
  }, []);

  useEffect(() => {
    let isActive = true;

    setHydrationStatus('hydrating');
    setHydrationError(null);

    void loadMeals()
      .then((persistedMeals) => {
        if (!isActive) {
          return;
        }

        setMeals(persistedMeals);
        setHydrationStatus('ready');
      })
      .catch((error: unknown) => {
        if (!isActive) {
          return;
        }

        setHydrationError(error instanceof Error ? error : new Error('Meal hydration failed.'));
        setHydrationStatus('error');
      });

    return () => {
      isActive = false;
    };
  }, [hydrationAttempt]);

  const value = useMemo(
    () => ({ meals, hydrationStatus, hydrationError, retryHydration }),
    [meals, hydrationStatus, hydrationError, retryHydration],
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
