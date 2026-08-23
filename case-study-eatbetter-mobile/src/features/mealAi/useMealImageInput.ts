import { useCallback, useEffect, useRef, useState } from 'react';
import * as ImagePicker from 'expo-image-picker';
import { Platform } from 'react-native';

import type { PreparedMealImage } from '../../domain/mealImage';
import type { MealImageInputError } from './mealImageErrorPresentation';
import {
  MealImagePreparationError,
  prepareMealImage,
  releasePreparedMealImage,
} from './prepareMealImage';

export type MealImageInputOperation = 'idle' | 'acquiring' | 'preparing';

export type MealImageInputState = {
  image: PreparedMealImage | null;
  operation: MealImageInputOperation;
  error: MealImageInputError | null;
};

export type UseMealImageInputResult = MealImageInputState & {
  takePhoto: () => Promise<void>;
  selectFromGallery: () => Promise<void>;
  removeImage: () => void;
  clearImage: () => boolean;
  protectImageForUpload: (image: PreparedMealImage) => boolean;
  unprotectImageAfterUpload: (image: PreparedMealImage) => void;
};

const PICKER_OPTIONS: ImagePicker.ImagePickerOptions = {
  mediaTypes: ['images'],
  allowsEditing: false,
  allowsMultipleSelection: false,
  quality: 1,
  base64: false,
  exif: false,
};

function toPreparationError(error: unknown): MealImagePreparationError {
  if (error instanceof MealImagePreparationError) {
    return error;
  }

  return new MealImagePreparationError(
    'processing_failed',
    'The image could not be prepared.',
    { cause: error },
  );
}

function isPickerErrorResult(
  result: ImagePicker.ImagePickerResult | ImagePicker.ImagePickerErrorResult,
): result is ImagePicker.ImagePickerErrorResult {
  return 'code' in result;
}

export function useMealImageInput(): UseMealImageInputResult {
  const [state, setState] = useState<MealImageInputState>({
    image: null,
    operation: 'idle',
    error: null,
  });
  const mountedRef = useRef(true);
  const generationRef = useRef(0);
  const commandInFlightRef = useRef(false);
  const pendingRecoveryStartedRef = useRef(false);
  const imageRef = useRef<PreparedMealImage | null>(null);
  const uploadProtectedImageRef = useRef<PreparedMealImage | null>(null);

  const ownsCommand = useCallback((generation: number): boolean => {
    return mountedRef.current && generationRef.current === generation;
  }, []);

  const releaseIfSafe = useCallback((image: PreparedMealImage | null): void => {
    if (image !== null && uploadProtectedImageRef.current !== image) {
      releasePreparedMealImage(image);
    }
  }, []);

  const consumePickerResult = useCallback(
    async (
      result: ImagePicker.ImagePickerResult | ImagePicker.ImagePickerErrorResult,
      generation: number,
    ): Promise<void> => {
      if (!ownsCommand(generation)) {
        return;
      }

      if (isPickerErrorResult(result)) {
        setState((current) => ({
          ...current,
          operation: 'idle',
          error: { kind: 'acquisition_failed' },
        }));
        return;
      }

      if (result.canceled) {
        setState((current) => ({ ...current, operation: 'idle' }));
        return;
      }

      if (result.assets.length !== 1) {
        throw new MealImagePreparationError(
          'invalid_source',
          'A single local image is required.',
        );
      }

      setState((current) => ({ ...current, operation: 'preparing' }));
      let preparedImage: PreparedMealImage;
      try {
        preparedImage = await prepareMealImage(result.assets[0].uri);
      } catch (error) {
        if (ownsCommand(generation)) {
          setState((current) => ({
            ...current,
            operation: 'idle',
            error: { kind: 'preparation_failed', error: toPreparationError(error) },
          }));
        }
        return;
      }

      if (!ownsCommand(generation)) {
        releasePreparedMealImage(preparedImage);
        return;
      }

      const previousImage = imageRef.current;
      imageRef.current = preparedImage;
      setState({ image: preparedImage, operation: 'idle', error: null });
      releaseIfSafe(previousImage);
    },
    [ownsCommand, releaseIfSafe],
  );

  const runPickerCommand = useCallback(
    async (source: 'camera' | 'gallery'): Promise<void> => {
      if (
        !mountedRef.current ||
        commandInFlightRef.current ||
        uploadProtectedImageRef.current !== null
      ) {
        return;
      }

      commandInFlightRef.current = true;
      const generation = ++generationRef.current;
      setState((current) => ({ ...current, operation: 'acquiring', error: null }));

      try {
        if (source === 'camera') {
          let permission = await ImagePicker.getCameraPermissionsAsync();
          if (!ownsCommand(generation)) {
            return;
          }

          if (!permission.granted && permission.canAskAgain) {
            permission = await ImagePicker.requestCameraPermissionsAsync();
          }

          if (!ownsCommand(generation)) {
            return;
          }

          if (!permission.granted) {
            setState((current) => ({
              ...current,
              operation: 'idle',
              error: { kind: 'camera_permission_denied' },
            }));
            return;
          }
        }

        const result =
          source === 'camera'
            ? await ImagePicker.launchCameraAsync(PICKER_OPTIONS)
            : await ImagePicker.launchImageLibraryAsync(PICKER_OPTIONS);
        if (!ownsCommand(generation)) {
          return;
        }
        await consumePickerResult(result, generation);
      } catch (error) {
        if (ownsCommand(generation)) {
          setState((current) => ({
            ...current,
            operation: 'idle',
            error:
              error instanceof MealImagePreparationError
                ? { kind: 'preparation_failed', error }
                : { kind: 'acquisition_failed' },
          }));
        }
      } finally {
        if (ownsCommand(generation)) {
          commandInFlightRef.current = false;
        }
      }
    },
    [consumePickerResult, ownsCommand],
  );

  const recoverPendingPickerResult = useCallback(async (): Promise<void> => {
    if (
      Platform.OS !== 'android' ||
      !mountedRef.current ||
      pendingRecoveryStartedRef.current ||
      commandInFlightRef.current ||
      uploadProtectedImageRef.current !== null
    ) {
      return;
    }

    pendingRecoveryStartedRef.current = true;
    const generation = ++generationRef.current;

    try {
      const result = await ImagePicker.getPendingResultAsync();
      if (!ownsCommand(generation) || result === null) {
        return;
      }

      commandInFlightRef.current = true;
      await consumePickerResult(result, generation);
    } catch (error) {
      if (ownsCommand(generation)) {
        setState((current) => ({
          ...current,
          operation: 'idle',
          error:
            error instanceof MealImagePreparationError
              ? { kind: 'preparation_failed', error }
              : { kind: 'acquisition_failed' },
        }));
      }
    } finally {
      if (ownsCommand(generation)) {
        commandInFlightRef.current = false;
      }
    }
  }, [consumePickerResult, ownsCommand]);

  const takePhoto = useCallback(
    (): Promise<void> => runPickerCommand('camera'),
    [runPickerCommand],
  );

  const selectFromGallery = useCallback(
    (): Promise<void> => runPickerCommand('gallery'),
    [runPickerCommand],
  );

  const clearImage = useCallback((): boolean => {
    if (commandInFlightRef.current || uploadProtectedImageRef.current !== null) {
      return false;
    }

    generationRef.current += 1;
    commandInFlightRef.current = false;
    const currentImage = imageRef.current;
    imageRef.current = null;
    if (mountedRef.current) {
      setState({ image: null, operation: 'idle', error: null });
    }
    releaseIfSafe(currentImage);
    return true;
  }, [releaseIfSafe]);

  const removeImage = useCallback((): void => {
    if (state.operation === 'idle' && uploadProtectedImageRef.current === null) {
      clearImage();
    }
  }, [clearImage, state.operation]);

  const protectImageForUpload = useCallback((image: PreparedMealImage): boolean => {
    if (
      imageRef.current !== image ||
      commandInFlightRef.current ||
      uploadProtectedImageRef.current !== null
    ) {
      return false;
    }

    uploadProtectedImageRef.current = image;
    return true;
  }, []);

  const unprotectImageAfterUpload = useCallback((image: PreparedMealImage): void => {
    if (uploadProtectedImageRef.current === image) {
      uploadProtectedImageRef.current = null;
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    const pendingRecoveryTimer =
      Platform.OS === 'android'
        ? setTimeout(() => void recoverPendingPickerResult(), 0)
        : null;

    return () => {
      if (pendingRecoveryTimer !== null) {
        clearTimeout(pendingRecoveryTimer);
      }
      mountedRef.current = false;
      generationRef.current += 1;
      commandInFlightRef.current = false;
      const currentImage = imageRef.current;
      imageRef.current = null;
      releaseIfSafe(currentImage);
    };
  }, [recoverPendingPickerResult, releaseIfSafe]);

  return {
    ...state,
    takePhoto,
    selectFromGallery,
    removeImage,
    clearImage,
    protectImageForUpload,
    unprotectImageAfterUpload,
  };
}
