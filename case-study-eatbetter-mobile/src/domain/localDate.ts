import type { MealRecord } from './meal';

export function createLocalDateKey(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');

  return `${year}-${month}-${day}`;
}

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
