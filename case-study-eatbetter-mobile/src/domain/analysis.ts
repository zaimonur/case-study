import {
  calculateDailyNutritionAggregate,
  type AggregateMetric,
  type DailyNutritionAggregate,
} from './dailyTotals';
import type { MealRecord } from './meal';
import { createLocalDateKey } from './localDate';

export type AnalysisAverageMetricStatus =
  | 'exact'
  | 'partial'
  | 'unavailable'
  | 'no-data';

export type AnalysisAverageMetric = {
  value: number | null;
  status: AnalysisAverageMetricStatus;
};

export type AnalysisAverageNutrition = {
  caloriesKcal: AnalysisAverageMetric;
  proteinG: AnalysisAverageMetric;
  carbohydratesG: AnalysisAverageMetric;
  fatG: AnalysisAverageMetric;
};

export type NutritionCoverageMetric = {
  knownItemCount: number;
  unknownItemCount: number;
};

export type NutritionCoverage = {
  caloriesKcal: NutritionCoverageMetric;
  proteinG: NutritionCoverageMetric;
  carbohydratesG: NutritionCoverageMetric;
  fatG: NutritionCoverageMetric;
};

export type SevenDayAnalysisDay = {
  date: Date;
  localDateKey: string;
  hasLoggedData: boolean;
  mealCount: number;
  itemCount: number;
  nutrition: DailyNutritionAggregate;
};

export type SevenDayAnalysis = {
  days: SevenDayAnalysisDay[];
  loggedDayCount: number;
  mealCount: number;
  itemCount: number;
  averageNutrition: AnalysisAverageNutrition;
  nutritionCoverage: NutritionCoverage;
};

type NutritionMetricKey = keyof DailyNutritionAggregate;

const NUTRITION_METRIC_KEYS: NutritionMetricKey[] = [
  'caloriesKcal',
  'proteinG',
  'carbohydratesG',
  'fatG',
];

function createLocalCalendarDay(referenceDate: Date, daysAgo: number): Date {
  return new Date(
    referenceDate.getFullYear(),
    referenceDate.getMonth(),
    referenceDate.getDate() - daysAgo,
  );
}

function createAverageMetric(
  metric: AggregateMetric,
  loggedDayCount: number,
): AnalysisAverageMetric {
  if (loggedDayCount === 0) {
    return { value: null, status: 'no-data' };
  }

  if (metric.knownItemCount === 0) {
    return { value: null, status: 'unavailable' };
  }

  return {
    value: metric.knownTotal / loggedDayCount,
    status: metric.unknownItemCount > 0 ? 'partial' : 'exact',
  };
}

function createCoverageMetric(
  metric: AggregateMetric,
): NutritionCoverageMetric {
  return {
    knownItemCount: metric.knownItemCount,
    unknownItemCount: metric.unknownItemCount,
  };
}

function summarizeNutrition(
  days: SevenDayAnalysisDay[],
  loggedDayCount: number,
): Pick<SevenDayAnalysis, 'averageNutrition' | 'nutritionCoverage'> {
  const aggregate = Object.fromEntries(
    NUTRITION_METRIC_KEYS.map((key) => [
      key,
      days.reduce<AggregateMetric>(
        (total, day) => ({
          knownTotal: total.knownTotal + day.nutrition[key].knownTotal,
          knownItemCount:
            total.knownItemCount + day.nutrition[key].knownItemCount,
          unknownItemCount:
            total.unknownItemCount + day.nutrition[key].unknownItemCount,
        }),
        { knownTotal: 0, knownItemCount: 0, unknownItemCount: 0 },
      ),
    ]),
  ) as DailyNutritionAggregate;

  const averageNutrition: AnalysisAverageNutrition = {
    caloriesKcal: createAverageMetric(aggregate.caloriesKcal, loggedDayCount),
    proteinG: createAverageMetric(aggregate.proteinG, loggedDayCount),
    carbohydratesG: createAverageMetric(
      aggregate.carbohydratesG,
      loggedDayCount,
    ),
    fatG: createAverageMetric(aggregate.fatG, loggedDayCount),
  };
  const nutritionCoverage: NutritionCoverage = {
    caloriesKcal: createCoverageMetric(aggregate.caloriesKcal),
    proteinG: createCoverageMetric(aggregate.proteinG),
    carbohydratesG: createCoverageMetric(aggregate.carbohydratesG),
    fatG: createCoverageMetric(aggregate.fatG),
  };

  return { averageNutrition, nutritionCoverage };
}

/**
 * Produces today through six local calendar days ago, ordered newest to oldest.
 */
export function analyzeLastSevenLocalDays(
  meals: MealRecord[],
  referenceDate: Date,
): SevenDayAnalysis {
  if (Number.isNaN(referenceDate.getTime())) {
    throw new RangeError('referenceDate must be a valid Date');
  }

  const calendarDays = Array.from({ length: 7 }, (_, index) => {
    const date = createLocalCalendarDay(referenceDate, index);

    return {
      date,
      localDateKey: createLocalDateKey(date),
    };
  });
  const includedLocalDateKeys = new Set(
    calendarDays.map((day) => day.localDateKey),
  );

  const mealsByLocalDate = new Map<string, MealRecord[]>();

  meals.forEach((meal) => {
    if (meal.items.length === 0) {
      return;
    }

    const loggedDate = new Date(meal.loggedAt);

    if (Number.isNaN(loggedDate.getTime())) {
      return;
    }

    const localDateKey = createLocalDateKey(loggedDate);

    if (!includedLocalDateKeys.has(localDateKey)) {
      return;
    }

    const dayMeals = mealsByLocalDate.get(localDateKey);

    if (dayMeals) {
      dayMeals.push(meal);
    } else {
      mealsByLocalDate.set(localDateKey, [meal]);
    }
  });

  const days: SevenDayAnalysisDay[] = calendarDays.map(
    ({ date, localDateKey }) => {
      const dayMeals = mealsByLocalDate.get(localDateKey) ?? [];

      return {
        date,
        localDateKey,
        hasLoggedData: dayMeals.length > 0,
        mealCount: dayMeals.length,
        itemCount: dayMeals.reduce(
          (total, meal) => total + meal.items.length,
          0,
        ),
        nutrition: calculateDailyNutritionAggregate(dayMeals),
      };
    },
  );

  const loggedDayCount = days.reduce(
    (total, day) => total + (day.hasLoggedData ? 1 : 0),
    0,
  );
  const mealCount = days.reduce((total, day) => total + day.mealCount, 0);
  const itemCount = days.reduce((total, day) => total + day.itemCount, 0);
  const { averageNutrition, nutritionCoverage } = summarizeNutrition(
    days,
    loggedDayCount,
  );

  return {
    days,
    loggedDayCount,
    mealCount,
    itemCount,
    averageNutrition,
    nutritionCoverage,
  };
}
