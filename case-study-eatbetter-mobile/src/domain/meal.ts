import type { NutritionValues } from './nutrition';

export type GramsSelectionSnapshot = {
  kind: 'grams';
  grams: number;
};

export type PortionSelectionSnapshot = {
  kind: 'portion';
  portionId: string;
  quantity: number;
  amount: number;
  measure: string;
  portionGrams: number;
};

export type MealSelectionSnapshot = GramsSelectionSnapshot | PortionSelectionSnapshot;

export type MealItem = {
  foodId: string;
  displayName: string;
  canonicalName: string;
  brand: string | null;
  resolvedGrams: number;
  nutrition: NutritionValues;
  selection: MealSelectionSnapshot;
};

export type MealRecord = {
  id: string;
  loggedAt: string;
  items: MealItem[];
};
