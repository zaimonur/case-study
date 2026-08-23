import { ApiError } from '../../api/client';

export type MealAiErrorPresentation = {
  message: string;
  retryable: boolean;
};

export function getMealAiErrorPresentation(error: Error): MealAiErrorPresentation {
  if (!(error instanceof ApiError)) {
    return {
      message: 'Bu işlem şu anda tamamlanamıyor.',
      retryable: false,
    };
  }

  if (error.kind === 'network') {
    return {
      message: 'Bağlantı kurulamadı. İnternet bağlantını kontrol edip tekrar deneyebilirsin.',
      retryable: true,
    };
  }

  if (error.kind === 'timeout' || (error.kind === 'http' && error.httpStatus === 408)) {
    return {
      message: 'İstek zaman aşımına uğradı. Tekrar deneyebilirsin.',
      retryable: true,
    };
  }

  if (error.kind === 'http' && error.httpStatus === 429) {
    return {
      message: 'Çok fazla istek gönderildi. Biraz bekleyip tekrar deneyebilirsin.',
      retryable: true,
    };
  }

  if (error.kind === 'http' && error.httpStatus !== undefined && error.httpStatus >= 500) {
    return {
      message: 'Servis şu anda yanıt veremiyor. Biraz sonra tekrar deneyebilirsin.',
      retryable: true,
    };
  }

  if (error.kind === 'invalid-response') {
    return {
      message: 'Sunucudan gelen yanıt doğrulanamadı. Tekrar deneyebilirsin.',
      retryable: true,
    };
  }

  return {
    message: 'Bu işlem şu anda tamamlanamıyor.',
    retryable: false,
  };
}
