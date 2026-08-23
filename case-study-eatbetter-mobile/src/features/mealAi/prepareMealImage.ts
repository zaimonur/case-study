import { File } from 'expo-file-system';
import { ImageManipulator, SaveFormat } from 'expo-image-manipulator';

import {
  MEAL_IMAGE_MAX_BYTES,
  type PreparedMealImage,
} from '../../domain/mealImage';

export type MealImagePreparationErrorCode =
  | 'invalid_source'
  | 'processing_failed'
  | 'payload_too_large';

export class MealImagePreparationError extends Error {
  readonly code: MealImagePreparationErrorCode;

  constructor(
    code: MealImagePreparationErrorCode,
    message: string,
    options?: { cause?: unknown },
  ) {
    super(message, options);
    this.name = 'MealImagePreparationError';
    this.code = code;
  }
}

type PreparationPolicy = {
  maximumLongEdge: number;
  compression: number;
};

type GeneratedMealImage = {
  uri: string;
  mimeType: 'image/jpeg';
  sizeBytes: number;
  width: number;
  height: number;
};

const FIRST_PASS_POLICY: PreparationPolicy = {
  maximumLongEdge: 2048,
  compression: 0.8,
};

const FALLBACK_POLICY: PreparationPolicy = {
  maximumLongEdge: 1600,
  compression: 0.65,
};

function isPositiveSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}

function isLocalFileUri(value: unknown): value is string {
  if (typeof value !== 'string' || value.trim() !== value || value.length === 0) {
    return false;
  }

  try {
    return new URL(value).protocol === 'file:';
  } catch {
    return false;
  }
}

function safeRelease(resource: { release(): void } | null): void {
  try {
    resource?.release();
  } catch {
    // Native image resources are best-effort cleanup and must not mask the result.
  }
}

function bestEffortDeleteGeneratedFile(uri: string | null): void {
  if (uri === null || !isLocalFileUri(uri)) {
    return;
  }

  try {
    const file = new File(uri);
    if (file.exists) {
      file.delete();
    }
  } catch {
    // Cache cleanup failure must not fail otherwise successful preparation.
  }
}

function assertPreparedDimensions(width: unknown, height: unknown): void {
  if (!isPositiveSafeInteger(width) || !isPositiveSafeInteger(height)) {
    throw new MealImagePreparationError(
      'processing_failed',
      'The prepared image dimensions are invalid.',
    );
  }
}

function measurePreparedFile(uri: unknown): { uri: string; sizeBytes: number } {
  if (!isLocalFileUri(uri)) {
    throw new MealImagePreparationError(
      'processing_failed',
      'The prepared image file is unavailable.',
    );
  }

  const file = new File(uri);
  const info = file.info();
  if (!info.exists || !isPositiveSafeInteger(info.size) || file.type !== 'image/jpeg') {
    throw new MealImagePreparationError(
      'processing_failed',
      'The prepared image file is invalid.',
    );
  }

  return { uri, sizeBytes: info.size };
}

async function runPreparationPass(
  decodedImage: Parameters<typeof ImageManipulator.manipulate>[0],
  sourceWidth: number,
  sourceHeight: number,
  policy: PreparationPolicy,
): Promise<GeneratedMealImage> {
  const context = ImageManipulator.manipulate(decodedImage);
  let renderedImage: Awaited<ReturnType<typeof context.renderAsync>> | null = null;
  let generatedUri: string | null = null;

  try {
    if (Math.max(sourceWidth, sourceHeight) > policy.maximumLongEdge) {
      if (sourceWidth >= sourceHeight) {
        context.resize({ width: policy.maximumLongEdge });
      } else {
        context.resize({ height: policy.maximumLongEdge });
      }
    }

    renderedImage = await context.renderAsync();
    assertPreparedDimensions(renderedImage.width, renderedImage.height);

    const savedImage = await renderedImage.saveAsync({
      format: SaveFormat.JPEG,
      compress: policy.compression,
    });
    generatedUri = savedImage.uri;
    assertPreparedDimensions(savedImage.width, savedImage.height);

    const measuredFile = measurePreparedFile(savedImage.uri);
    return {
      uri: measuredFile.uri,
      mimeType: 'image/jpeg',
      sizeBytes: measuredFile.sizeBytes,
      width: savedImage.width,
      height: savedImage.height,
    };
  } catch (error) {
    bestEffortDeleteGeneratedFile(generatedUri);
    throw error;
  } finally {
    safeRelease(renderedImage);
    safeRelease(context);
  }
}

export async function prepareMealImage(sourceUri: string): Promise<PreparedMealImage> {
  if (!isLocalFileUri(sourceUri)) {
    throw new MealImagePreparationError(
      'invalid_source',
      'A valid local image is required.',
    );
  }

  try {
    const sourceFile = new File(sourceUri);
    if (!sourceFile.exists) {
      throw new MealImagePreparationError(
        'invalid_source',
        'A valid local image is required.',
      );
    }
  } catch (error) {
    if (error instanceof MealImagePreparationError) {
      throw error;
    }

    throw new MealImagePreparationError(
      'invalid_source',
      'A valid local image is required.',
      { cause: error },
    );
  }

  const decodeContext = ImageManipulator.manipulate(sourceUri);
  let decodedImage: Awaited<ReturnType<typeof decodeContext.renderAsync>> | null = null;

  try {
    decodedImage = await decodeContext.renderAsync();
    assertPreparedDimensions(decodedImage.width, decodedImage.height);

    const firstPass = await runPreparationPass(
      decodedImage,
      decodedImage.width,
      decodedImage.height,
      FIRST_PASS_POLICY,
    );
    if (firstPass.sizeBytes <= MEAL_IMAGE_MAX_BYTES) {
      return firstPass;
    }

    bestEffortDeleteGeneratedFile(firstPass.uri);
    const fallback = await runPreparationPass(
      decodedImage,
      decodedImage.width,
      decodedImage.height,
      FALLBACK_POLICY,
    );
    if (fallback.sizeBytes <= MEAL_IMAGE_MAX_BYTES) {
      return fallback;
    }

    bestEffortDeleteGeneratedFile(fallback.uri);
    throw new MealImagePreparationError(
      'payload_too_large',
      'The prepared image is too large to upload.',
    );
  } catch (error) {
    if (error instanceof MealImagePreparationError) {
      throw error;
    }

    throw new MealImagePreparationError(
      'processing_failed',
      'The image could not be prepared.',
      { cause: error },
    );
  } finally {
    safeRelease(decodedImage);
    safeRelease(decodeContext);
  }
}
