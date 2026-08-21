export type NutritionValues = {
  caloriesKcal: number | null;
  proteinG: number | null;
  carbohydratesG: number | null;
  fatG: number | null;
};

export type CalculatedNutrition = {
  foodId: number;
  resolvedGrams: number;
  nutrition: NutritionValues;
};
