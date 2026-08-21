import type { CalculatedNutrition, NutritionValues } from '../domain/nutrition';
import { ApiError, postJson } from './client';

export type CalculateNutritionInput =
  | {
      foodId: number;
      kind: 'grams';
      grams: number;
    }
  | {
      foodId: number;
      kind: 'portion';
      portionId: number;
      quantity: number;
    };

type NutritionValuesDto = {
  calories_kcal: number | null;
  protein_g: number | null;
  carbohydrates_g: number | null;
  fat_g: number | null;
};

type CalculatedNutritionDto = {
  food_id: number;
  resolved_grams: number;
  nutrition: NutritionValuesDto;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isPositiveSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}

function isPositiveFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0;
}

function isNullableFiniteNumber(value: unknown): value is number | null {
  return value === null || (typeof value === 'number' && Number.isFinite(value));
}

function isNutritionValuesDto(value: unknown): value is NutritionValuesDto {
  return (
    isRecord(value) &&
    isNullableFiniteNumber(value.calories_kcal) &&
    isNullableFiniteNumber(value.protein_g) &&
    isNullableFiniteNumber(value.carbohydrates_g) &&
    isNullableFiniteNumber(value.fat_g)
  );
}

function isCalculatedNutritionDto(value: unknown): value is CalculatedNutritionDto {
  return (
    isRecord(value) &&
    isPositiveSafeInteger(value.food_id) &&
    isPositiveFiniteNumber(value.resolved_grams) &&
    isNutritionValuesDto(value.nutrition)
  );
}

function mapNutritionValues(dto: NutritionValuesDto): NutritionValues {
  return {
    caloriesKcal: dto.calories_kcal,
    proteinG: dto.protein_g,
    carbohydratesG: dto.carbohydrates_g,
    fatG: dto.fat_g,
  };
}

export async function calculateNutrition(
  input: CalculateNutritionInput,
  signal?: AbortSignal,
): Promise<CalculatedNutrition> {
  if (!isPositiveSafeInteger(input.foodId)) {
    throw new ApiError('config', 'The calculation food ID is invalid.');
  }

  let requestBody: Record<string, number>;

  if (input.kind === 'grams') {
    if (!isPositiveFiniteNumber(input.grams)) {
      throw new ApiError('config', 'The calculation grams value is invalid.');
    }

    requestBody = {
      food_id: input.foodId,
      grams: input.grams,
    };
  } else {
    if (
      !isPositiveSafeInteger(input.portionId) ||
      !isPositiveFiniteNumber(input.quantity)
    ) {
      throw new ApiError('config', 'The calculation portion selection is invalid.');
    }

    requestBody = {
      food_id: input.foodId,
      portion_id: input.portionId,
      quantity: input.quantity,
    };
  }

  const { data, httpStatus, requestId } = await postJson('/nutrition/calculate', {
    body: requestBody,
    signal,
  });

  if (!isCalculatedNutritionDto(data) || data.food_id !== input.foodId) {
    throw new ApiError('invalid-response', 'The calculated nutrition response is invalid.', {
      httpStatus,
      requestId,
    });
  }

  return {
    foodId: data.food_id,
    resolvedGrams: data.resolved_grams,
    nutrition: mapNutritionValues(data.nutrition),
  };
}
