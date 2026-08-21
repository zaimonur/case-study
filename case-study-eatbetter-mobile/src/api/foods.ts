import type { FoodSearchItem } from '../domain/food';
import { ApiError, getJson } from './client';

type FoodSearchDto = {
  food_id: number;
  display_name: string;
  canonical_name: string;
  brand: string | null;
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
