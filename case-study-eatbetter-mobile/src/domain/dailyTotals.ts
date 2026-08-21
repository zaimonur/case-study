import type { MealRecord } from './meal';
import type { NutritionValues } from './nutrition';

function addNutrient(total: number | null, value: number | null): number | null {
  if (total === null || value === null) {
    return null;
  }

  return total + value;
}

export function calculateDailyTotals(meals: MealRecord[]): NutritionValues {
  const totals: NutritionValues = {
    caloriesKcal: 0,
    proteinG: 0,
    carbohydratesG: 0,
    fatG: 0,
  };

  meals.forEach((meal) => {
    meal.items.forEach((item) => {
      totals.caloriesKcal = addNutrient(totals.caloriesKcal, item.nutrition.caloriesKcal);
      totals.proteinG = addNutrient(totals.proteinG, item.nutrition.proteinG);
      totals.carbohydratesG = addNutrient(
        totals.carbohydratesG,
        item.nutrition.carbohydratesG,
      );
      totals.fatG = addNutrient(totals.fatG, item.nutrition.fatG);
    });
  });

  return totals;
}
