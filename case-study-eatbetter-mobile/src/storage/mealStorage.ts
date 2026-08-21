import AsyncStorage from '@react-native-async-storage/async-storage';

import type { MealItem, MealRecord, MealSelectionSnapshot } from '../domain/meal';
import type { NutritionValues } from '../domain/nutrition';

export const MEAL_STORAGE_KEY = 'eatbetter.meals.v1';

export class MealStorageError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = 'MealStorageError';
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

function isNullableFiniteNumber(value: unknown): value is number | null {
  return value === null || isFiniteNumber(value);
}

function isNutritionValues(value: unknown): value is NutritionValues {
  return (
    isRecord(value) &&
    isNullableFiniteNumber(value.caloriesKcal) &&
    isNullableFiniteNumber(value.proteinG) &&
    isNullableFiniteNumber(value.carbohydratesG) &&
    isNullableFiniteNumber(value.fatG)
  );
}

function isSelectionSnapshot(value: unknown): value is MealSelectionSnapshot {
  if (!isRecord(value)) {
    return false;
  }

  if (value.kind === 'grams') {
    return isFiniteNumber(value.grams);
  }

  if (value.kind === 'portion') {
    return (
      isFiniteNumber(value.portionId) &&
      isFiniteNumber(value.quantity) &&
      isFiniteNumber(value.amount) &&
      typeof value.measure === 'string' &&
      isFiniteNumber(value.portionGrams)
    );
  }

  return false;
}

function isMealItem(value: unknown): value is MealItem {
  return (
    isRecord(value) &&
    isFiniteNumber(value.foodId) &&
    typeof value.displayName === 'string' &&
    typeof value.canonicalName === 'string' &&
    (typeof value.brand === 'string' || value.brand === null) &&
    isFiniteNumber(value.resolvedGrams) &&
    isNutritionValues(value.nutrition) &&
    isSelectionSnapshot(value.selection)
  );
}

function isMealRecord(value: unknown): value is MealRecord {
  return (
    isRecord(value) &&
    typeof value.id === 'string' &&
    typeof value.loggedAt === 'string' &&
    Array.isArray(value.items) &&
    value.items.every(isMealItem)
  );
}

function parseMeals(rawValue: string): MealRecord[] {
  let parsed: unknown;

  try {
    parsed = JSON.parse(rawValue);
  } catch (error) {
    throw new MealStorageError('Persisted meal data is not valid JSON.', { cause: error });
  }

  if (!Array.isArray(parsed) || !parsed.every(isMealRecord)) {
    throw new MealStorageError('Persisted meal data has an invalid shape.');
  }

  return parsed;
}

export async function loadMeals(): Promise<MealRecord[]> {
  try {
    const rawValue = await AsyncStorage.getItem(MEAL_STORAGE_KEY);

    return rawValue === null ? [] : parseMeals(rawValue);
  } catch (error) {
    if (error instanceof MealStorageError) {
      throw error;
    }

    throw new MealStorageError('Persisted meals could not be read.', { cause: error });
  }
}

export async function saveMeals(meals: MealRecord[]): Promise<void> {
  try {
    await AsyncStorage.setItem(MEAL_STORAGE_KEY, JSON.stringify(meals));
  } catch (error) {
    throw new MealStorageError('Meals could not be persisted.', { cause: error });
  }
}
