import type { MealItem, MealRecord } from './meal';

let mealIdSequence = 0;

function createLocalMealId(): string {
  mealIdSequence += 1;
  const randomPart = Math.random().toString(36).slice(2, 10);
  return `meal-${Date.now().toString(36)}-${mealIdSequence.toString(36)}-${randomPart}`;
}

export function createLocalMealRecord(items: MealItem[]): MealRecord {
  return {
    id: createLocalMealId(),
    loggedAt: new Date().toISOString(),
    items: [...items],
  };
}
