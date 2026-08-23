import type { MealItem, MealSelectionSnapshot } from '../../domain/meal';
import type { ReadyMealAiItem } from '../../domain/mealAi';

export function mapReadyMealAiItemToMealItem(item: ReadyMealAiItem): MealItem {
  const selection: MealSelectionSnapshot =
    item.selection.kind === 'grams'
      ? {
          kind: 'grams',
          grams: item.selection.grams,
        }
      : {
          kind: 'portion',
          portionId: item.selection.portion.portionId,
          quantity: item.selection.portion.quantity,
          amount: item.selection.portion.amount,
          measure: item.selection.portion.measure,
          portionGrams: item.selection.portion.portionGrams,
        };

  return {
    foodId: item.food.foodId,
    displayName: item.food.displayName,
    canonicalName: item.food.canonicalName,
    brand: item.food.brand,
    resolvedGrams: item.preview.resolvedGrams,
    nutrition: {
      caloriesKcal: item.preview.nutrition.caloriesKcal,
      proteinG: item.preview.nutrition.proteinG,
      carbohydratesG: item.preview.nutrition.carbohydratesG,
      fatG: item.preview.nutrition.fatG,
    },
    selection,
  };
}
