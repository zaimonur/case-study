import type { NutritionValues } from './nutrition';

export type FoodSearchItem = {
  foodId: number;
  displayName: string;
  canonicalName: string;
  brand: string | null;
};

export type FoodPortion = {
  portionId: number;
  amount: number;
  measure: string;
  grams: number;
};

export type FoodDetail = {
  foodId: number;
  displayName: string;
  canonicalName: string;
  brand: string | null;
  nutritionPer100g: NutritionValues;
  portions: FoodPortion[];
};
