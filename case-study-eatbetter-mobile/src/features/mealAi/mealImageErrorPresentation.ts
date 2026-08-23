import { MealImagePreparationError } from './prepareMealImage';

export type MealImageInputError =
  | { kind: 'camera_permission_denied' }
  | { kind: 'acquisition_failed' }
  | { kind: 'preparation_failed'; error: MealImagePreparationError };

export function getMealImageInputErrorMessage(error: MealImageInputError): string {
  if (error.kind === 'camera_permission_denied') {
    return 'Kamera izni verilmedi. İstersen galeriden bir fotoğraf seçebilirsin.';
  }

  if (error.kind === 'acquisition_failed') {
    return 'Fotoğraf alınamadı. Tekrar deneyebilirsin.';
  }

  if (error.error.code === 'invalid_source') {
    return 'Fotoğraf kullanılamadı. Başka bir fotoğraf deneyebilirsin.';
  }

  if (error.error.code === 'payload_too_large') {
    return 'Fotoğraf yükleme için hâlâ çok büyük. Başka bir fotoğraf deneyebilirsin.';
  }

  return 'Fotoğraf hazırlanamadı. Başka bir fotoğraf deneyebilirsin.';
}
