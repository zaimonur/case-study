import { analyzeLastSevenLocalDays } from '../../domain/analysis';
import type { AggregateMetric } from '../../domain/dailyTotals';
import { createLocalDateKey } from '../../domain/localDate';
import type {
  NutritionReport,
  NutritionReportItem,
  NutritionReportMeal,
  ReportLocalDate,
  ReportLocalDateTime,
  ReportSelection,
} from '../../domain/nutritionReport';
import type { MealItem, MealRecord } from '../../domain/meal';

type IncludedMeal = {
  readonly meal: MealRecord;
  readonly loggedDate: Date;
  readonly timestamp: number;
  readonly sourceIndex: number;
};

function assertValidDate(date: Date, name: string): void {
  if (Number.isNaN(date.getTime())) {
    throw new RangeError(`${name} must be a valid Date`);
  }
}

function copyLocalDate(date: Date): ReportLocalDate {
  return {
    localDateKey: createLocalDateKey(date),
    year: date.getFullYear(),
    month: date.getMonth() + 1,
    day: date.getDate(),
    weekday: date.getDay(),
  };
}

function copyLocalDateTime(date: Date, source: string): ReportLocalDateTime {
  return {
    ...copyLocalDate(date),
    source,
    hour: date.getHours(),
    minute: date.getMinutes(),
    second: date.getSeconds(),
  };
}

function copyAggregateMetric(metric: AggregateMetric): AggregateMetric {
  return {
    knownTotal: metric.knownTotal,
    knownItemCount: metric.knownItemCount,
    unknownItemCount: metric.unknownItemCount,
  };
}

function copySelection(item: MealItem): ReportSelection {
  if (item.selection.kind === 'grams') {
    return {
      kind: 'grams',
      grams: item.selection.grams,
    };
  }

  return {
    kind: 'portion',
    portionId: item.selection.portionId,
    quantity: item.selection.quantity,
    amount: item.selection.amount,
    measure: item.selection.measure,
    portionGrams: item.selection.portionGrams,
  };
}

function copyItem(item: MealItem): NutritionReportItem {
  return {
    foodId: item.foodId,
    displayName: item.displayName,
    canonicalName: item.canonicalName,
    brand: item.brand,
    resolvedGrams: item.resolvedGrams,
    selection: copySelection(item),
    nutrition: {
      caloriesKcal: item.nutrition.caloriesKcal,
      proteinG: item.nutrition.proteinG,
      carbohydratesG: item.nutrition.carbohydratesG,
      fatG: item.nutrition.fatG,
    },
  };
}

function copyMeal(includedMeal: IncludedMeal): NutritionReportMeal {
  return {
    id: includedMeal.meal.id,
    loggedAt: copyLocalDateTime(
      includedMeal.loggedDate,
      includedMeal.meal.loggedAt,
    ),
    items: includedMeal.meal.items.map(copyItem),
  };
}

export function buildSevenDayNutritionReport(
  meals: MealRecord[],
  referenceDate: Date,
  generatedAt: Date,
): NutritionReport {
  assertValidDate(referenceDate, 'referenceDate');
  assertValidDate(generatedAt, 'generatedAt');

  const analysis = analyzeLastSevenLocalDays(meals, referenceDate);
  const includedLocalDateKeys = new Set(
    analysis.days.map((day) => day.localDateKey),
  );
  const mealsByLocalDate = new Map<string, IncludedMeal[]>();

  meals.forEach((meal, sourceIndex) => {
    if (meal.items.length === 0) {
      return;
    }

    const loggedDate = new Date(meal.loggedAt);
    const timestamp = loggedDate.getTime();

    if (Number.isNaN(timestamp)) {
      return;
    }

    const localDateKey = createLocalDateKey(loggedDate);

    if (!includedLocalDateKeys.has(localDateKey)) {
      return;
    }

    const includedMeal = { meal, loggedDate, timestamp, sourceIndex };
    const dayMeals = mealsByLocalDate.get(localDateKey);

    if (dayMeals) {
      dayMeals.push(includedMeal);
    } else {
      mealsByLocalDate.set(localDateKey, [includedMeal]);
    }
  });

  const days = analysis.days
    .map((day) => {
      const dayMeals = [...(mealsByLocalDate.get(day.localDateKey) ?? [])]
        .sort(
          (left, right) =>
            left.timestamp - right.timestamp ||
            left.sourceIndex - right.sourceIndex,
        )
        .map(copyMeal);

      return {
        date: copyLocalDate(day.date),
        localDateKey: day.localDateKey,
        hasLoggedData: day.hasLoggedData,
        mealCount: day.mealCount,
        itemCount: day.itemCount,
        nutrition: {
          caloriesKcal: copyAggregateMetric(day.nutrition.caloriesKcal),
          proteinG: copyAggregateMetric(day.nutrition.proteinG),
          carbohydratesG: copyAggregateMetric(day.nutrition.carbohydratesG),
          fatG: copyAggregateMetric(day.nutrition.fatG),
        },
        meals: dayMeals,
      };
    })
    .reverse();

  const copyAverageMetric = (
    key: keyof typeof analysis.averageNutrition,
  ) => ({ ...analysis.averageNutrition[key] });
  const copyCoverageMetric = (
    key: keyof typeof analysis.nutritionCoverage,
  ) => ({ ...analysis.nutritionCoverage[key] });

  return {
    generatedAt: copyLocalDateTime(generatedAt, generatedAt.toISOString()),
    period: {
      startDate: { ...days[0].date },
      endDate: { ...days[days.length - 1].date },
    },
    summary: {
      loggedDayCount: analysis.loggedDayCount,
      mealCount: analysis.mealCount,
      itemCount: analysis.itemCount,
      averageNutrition: {
        caloriesKcal: copyAverageMetric('caloriesKcal'),
        proteinG: copyAverageMetric('proteinG'),
        carbohydratesG: copyAverageMetric('carbohydratesG'),
        fatG: copyAverageMetric('fatG'),
      },
      nutritionCoverage: {
        caloriesKcal: copyCoverageMetric('caloriesKcal'),
        proteinG: copyCoverageMetric('proteinG'),
        carbohydratesG: copyCoverageMetric('carbohydratesG'),
        fatG: copyCoverageMetric('fatG'),
      },
    },
    days,
  };
}
