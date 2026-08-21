import type { MealRecord } from './meal';

export function isSameLocalDay(date: Date, referenceDate: Date): boolean {
  if (Number.isNaN(date.getTime()) || Number.isNaN(referenceDate.getTime())) {
    return false;
  }

  return (
    date.getFullYear() === referenceDate.getFullYear() &&
    date.getMonth() === referenceDate.getMonth() &&
    date.getDate() === referenceDate.getDate()
  );
}

export function getMealsForLocalDay(
  meals: MealRecord[],
  referenceDate: Date,
): MealRecord[] {
  return meals
    .flatMap((meal) => {
      const loggedDate = new Date(meal.loggedAt);

      if (!isSameLocalDay(loggedDate, referenceDate)) {
        return [];
      }

      return [{ meal, timestamp: loggedDate.getTime() }];
    })
    .sort((left, right) => right.timestamp - left.timestamp)
    .map(({ meal }) => meal);
}
