import type { MealRecord } from './meal';

export type AggregateMetric = {
  knownTotal: number;
  knownItemCount: number;
  unknownItemCount: number;
};

export type DailyNutritionAggregate = {
  caloriesKcal: AggregateMetric;
  proteinG: AggregateMetric;
  carbohydratesG: AggregateMetric;
  fatG: AggregateMetric;
};

function createEmptyMetric(): AggregateMetric {
  return {
    knownTotal: 0,
    knownItemCount: 0,
    unknownItemCount: 0,
  };
}

function includeValue(metric: AggregateMetric, value: number | null): void {
  if (value === null) {
    metric.unknownItemCount += 1;
    return;
  }

  metric.knownTotal += value;
  metric.knownItemCount += 1;
}

export function calculateDailyNutritionAggregate(
  meals: MealRecord[],
): DailyNutritionAggregate {
  const aggregate: DailyNutritionAggregate = {
    caloriesKcal: createEmptyMetric(),
    proteinG: createEmptyMetric(),
    carbohydratesG: createEmptyMetric(),
    fatG: createEmptyMetric(),
  };

  meals.forEach((meal) => {
    meal.items.forEach((item) => {
      includeValue(aggregate.caloriesKcal, item.nutrition.caloriesKcal);
      includeValue(aggregate.proteinG, item.nutrition.proteinG);
      includeValue(aggregate.carbohydratesG, item.nutrition.carbohydratesG);
      includeValue(aggregate.fatG, item.nutrition.fatG);
    });
  });

  return aggregate;
}
