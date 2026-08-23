export const MEAL_IMAGE_MAX_BYTES = 8 * 1024 * 1024;

/** Transient, locally generated JPEG input for MealAI image interpretation. */
export type PreparedMealImage = {
  uri: string;
  mimeType: 'image/jpeg';
  sizeBytes: number;
  width: number;
  height: number;
};
