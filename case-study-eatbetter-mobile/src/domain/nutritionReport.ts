import type {
  AnalysisAverageNutrition,
  NutritionCoverage,
} from './analysis';
import type { DailyNutritionAggregate } from './dailyTotals';
import type { NutritionValues } from './nutrition';

export type ReportLocalDate = {
  readonly localDateKey: string;
  readonly year: number;
  readonly month: number;
  readonly day: number;
  readonly weekday: number;
};

export type ReportLocalDateTime = ReportLocalDate & {
  readonly source: string;
  readonly hour: number;
  readonly minute: number;
  readonly second: number;
};

export type ReportGramsSelection = {
  readonly kind: 'grams';
  readonly grams: number;
};

export type ReportPortionSelection = {
  readonly kind: 'portion';
  readonly portionId: number;
  readonly quantity: number;
  readonly amount: number;
  readonly measure: string;
  readonly portionGrams: number;
};

export type ReportSelection = ReportGramsSelection | ReportPortionSelection;

export type NutritionReportItem = {
  readonly foodId: number;
  readonly displayName: string;
  readonly canonicalName: string;
  readonly brand: string | null;
  readonly resolvedGrams: number;
  readonly selection: ReportSelection;
  readonly nutrition: NutritionValues;
};

export type NutritionReportMeal = {
  readonly id: string;
  readonly loggedAt: ReportLocalDateTime;
  readonly items: readonly NutritionReportItem[];
};

export type NutritionReportDay = {
  readonly date: ReportLocalDate;
  readonly localDateKey: string;
  readonly hasLoggedData: boolean;
  readonly mealCount: number;
  readonly itemCount: number;
  readonly nutrition: DailyNutritionAggregate;
  readonly meals: readonly NutritionReportMeal[];
};

export type NutritionReport = {
  readonly generatedAt: ReportLocalDateTime;
  readonly period: {
    readonly startDate: ReportLocalDate;
    readonly endDate: ReportLocalDate;
  };
  readonly summary: {
    readonly loggedDayCount: number;
    readonly mealCount: number;
    readonly itemCount: number;
    readonly averageNutrition: AnalysisAverageNutrition;
    readonly nutritionCoverage: NutritionCoverage;
  };
  readonly days: readonly NutritionReportDay[];
};
