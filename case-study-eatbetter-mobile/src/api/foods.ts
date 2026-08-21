import type { FoodDetail, FoodPortion, FoodSearchItem } from '../domain/food';
import type { NutritionValues } from '../domain/nutrition';
import { ApiError, getJson } from './client';

type FoodSearchDto = {
  food_id: number;
  display_name: string;
  canonical_name: string;
  brand: string | null;
};

type FoodPortionDto = {
  portion_id: number;
  amount: number;
  measure: string;
  grams: number;
};

type NutritionValuesDto = {
  calories_kcal: number | null;
  protein_g: number | null;
  carbohydrates_g: number | null;
  fat_g: number | null;
};

type FoodDetailDto = FoodSearchDto & {
  nutrition_per_100g: NutritionValuesDto;
  portions: FoodPortionDto[];
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isFoodSearchDto(value: unknown): value is FoodSearchDto {
  return (
    isRecord(value) &&
    typeof value.food_id === 'number' &&
    Number.isFinite(value.food_id) &&
    typeof value.display_name === 'string' &&
    typeof value.canonical_name === 'string' &&
    (typeof value.brand === 'string' || value.brand === null)
  );
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

function isFoodPortionDto(value: unknown): value is FoodPortionDto {
  return (
    isRecord(value) &&
    isPositiveSafeInteger(value.portion_id) &&
    isPositiveFiniteNumber(value.amount) &&
    typeof value.measure === 'string' &&
    isPositiveFiniteNumber(value.grams)
  );
}

function isFoodDetailDto(value: unknown): value is FoodDetailDto {
  return (
    isRecord(value) &&
    isPositiveSafeInteger(value.food_id) &&
    typeof value.display_name === 'string' &&
    typeof value.canonical_name === 'string' &&
    (typeof value.brand === 'string' || value.brand === null) &&
    isNutritionValuesDto(value.nutrition_per_100g) &&
    Array.isArray(value.portions) &&
    value.portions.every(isFoodPortionDto)
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

function mapFoodPortion(dto: FoodPortionDto): FoodPortion {
  return {
    portionId: dto.portion_id,
    amount: dto.amount,
    measure: dto.measure,
    grams: dto.grams,
  };
}

export async function searchFoods(query: string, signal?: AbortSignal): Promise<FoodSearchItem[]> {
  const { data, httpStatus, requestId } = await getJson('/foods/search', {
    query: {
      q: query,
      locale: 'tr-TR',
      limit: '15',
    },
    signal,
  });

  if (!isRecord(data) || !Array.isArray(data.items) || !data.items.every(isFoodSearchDto)) {
    throw new ApiError('invalid-response', 'The food search response has an invalid shape.', {
      httpStatus,
      requestId,
    });
  }

  return data.items.map((item) => ({
    foodId: item.food_id,
    displayName: item.display_name,
    canonicalName: item.canonical_name,
    brand: item.brand,
  }));
}

export async function getFood(foodId: number, signal?: AbortSignal): Promise<FoodDetail> {
  const { data, httpStatus, requestId } = await getJson(`/foods/${foodId}`, {
    query: { locale: 'tr-TR' },
    signal,
  });

  if (!isFoodDetailDto(data) || data.food_id !== foodId) {
    throw new ApiError('invalid-response', 'The food detail response has an invalid shape.', {
      httpStatus,
      requestId,
    });
  }

  return {
    foodId: data.food_id,
    displayName: data.display_name,
    canonicalName: data.canonical_name,
    brand: data.brand,
    nutritionPer100g: mapNutritionValues(data.nutrition_per_100g),
    portions: data.portions.map(mapFoodPortion),
  };
}
