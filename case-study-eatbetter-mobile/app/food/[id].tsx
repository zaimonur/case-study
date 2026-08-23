import { router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { isAbortError } from '../../src/api/client';
import { getFood } from '../../src/api/foods';
import {
  calculateNutrition,
  type CalculateNutritionInput,
} from '../../src/api/nutrition';
import type { FoodDetail } from '../../src/domain/food';
import type { MealItem, MealSelectionSnapshot } from '../../src/domain/meal';
import { createLocalMealRecord } from '../../src/domain/mealRecord';
import type { CalculatedNutrition } from '../../src/domain/nutrition';
import { CalculatedNutritionCard } from '../../src/features/food/CalculatedNutritionCard';
import { NutritionReference } from '../../src/features/food/NutritionReference';
import {
  PortionSelector,
  type PortionSelection,
} from '../../src/features/food/PortionSelector';
import { useMeals } from '../../src/state/MealStoreProvider';

type DetailRequestState = {
  status: 'invalid' | 'loading' | 'success' | 'error';
  foodId: number | null;
  food: FoodDetail | null;
};

type CalculationCandidate = {
  fingerprint: string;
  input: CalculateNutritionInput;
  selection: MealSelectionSnapshot;
};

type SuccessfulCalculation = {
  fingerprint: string;
  result: CalculatedNutrition;
  selection: MealSelectionSnapshot;
};

type CalculationState =
  | { status: 'idle' | 'calculating' | 'error'; success: null }
  | { status: 'success'; success: SuccessfulCalculation };

type SaveStatus = 'idle' | 'saving' | 'error';

function parseFoodId(id: string | string[] | undefined): number | null {
  const routeId = Array.isArray(id) ? id[0] : id;

  if (routeId === undefined || routeId.trim().length === 0) {
    return null;
  }

  const parsedFoodId = Number(routeId);
  return Number.isSafeInteger(parsedFoodId) && parsedFoodId > 0 ? parsedFoodId : null;
}

function parsePositiveNumberInput(value: string): number | null {
  const trimmedValue = value.trim();

  if (trimmedValue.length === 0) {
    return null;
  }

  const parsedValue = Number(trimmedValue);
  return Number.isFinite(parsedValue) && parsedValue > 0 ? parsedValue : null;
}

function getSelectionFingerprint(foodId: number | null, selection: PortionSelection): string {
  if (selection.kind === 'grams') {
    return JSON.stringify([foodId, selection.kind, selection.grams]);
  }

  if (selection.kind === 'portion') {
    return JSON.stringify([foodId, selection.kind, selection.portionId, selection.quantity]);
  }

  return JSON.stringify([foodId, selection.kind]);
}

function buildCalculationCandidate(
  food: FoodDetail,
  selection: PortionSelection,
  fingerprint: string,
): CalculationCandidate | null {
  if (selection.kind === 'grams') {
    const grams = parsePositiveNumberInput(selection.grams);

    if (grams === null) {
      return null;
    }

    return {
      fingerprint,
      input: { foodId: food.foodId, kind: 'grams', grams },
      selection: { kind: 'grams', grams },
    };
  }

  if (selection.kind === 'portion') {
    const portion = food.portions.find((item) => item.portionId === selection.portionId);
    const quantity = parsePositiveNumberInput(selection.quantity);

    if (portion === undefined || quantity === null) {
      return null;
    }

    return {
      fingerprint,
      input: {
        foodId: food.foodId,
        kind: 'portion',
        portionId: portion.portionId,
        quantity,
      },
      selection: {
        kind: 'portion',
        portionId: portion.portionId,
        quantity,
        amount: portion.amount,
        measure: portion.measure,
        portionGrams: portion.grams,
      },
    };
  }

  return null;
}

function mapManualCalculationToMealItem(
  food: FoodDetail,
  calculation: SuccessfulCalculation,
): MealItem {
  const selection: MealSelectionSnapshot =
    calculation.selection.kind === 'grams'
      ? { kind: 'grams', grams: calculation.selection.grams }
      : {
          kind: 'portion',
          portionId: calculation.selection.portionId,
          quantity: calculation.selection.quantity,
          amount: calculation.selection.amount,
          measure: calculation.selection.measure,
          portionGrams: calculation.selection.portionGrams,
        };

  return {
    foodId: food.foodId,
    displayName: food.displayName,
    canonicalName: food.canonicalName,
    brand: food.brand,
    resolvedGrams: calculation.result.resolvedGrams,
    nutrition: {
      caloriesKcal: calculation.result.nutrition.caloriesKcal,
      proteinG: calculation.result.nutrition.proteinG,
      carbohydratesG: calculation.result.nutrition.carbohydratesG,
      fatG: calculation.result.nutrition.fatG,
    },
    selection,
  };
}

export default function FoodDetailScreen() {
  const { id } = useLocalSearchParams<{ id?: string | string[] }>();
  const foodId = parseFoodId(id);
  const { addMeal, hydrationStatus } = useMeals();

  const activeFoodIdRef = useRef(foodId);
  const detailGenerationRef = useRef(0);
  const detailAbortControllerRef = useRef<AbortController | null>(null);
  const calculationGenerationRef = useRef(0);
  const calculationAbortControllerRef = useRef<AbortController | null>(null);
  const calculationInFlightRef = useRef(false);
  const saveInFlightRef = useRef(false);

  const [requestState, setRequestState] = useState<DetailRequestState>({
    status: foodId === null ? 'invalid' : 'loading',
    foodId,
    food: null,
  });
  const [selection, setSelection] = useState<PortionSelection>({ kind: 'none' });
  const [calculationState, setCalculationState] = useState<CalculationState>({
    status: 'idle',
    success: null,
  });
  const [saveStatus, setSaveStatus] = useState<SaveStatus>('idle');

  const selectionFingerprint = getSelectionFingerprint(foodId, selection);
  const activeSelectionFingerprintRef = useRef(selectionFingerprint);

  activeFoodIdRef.current = foodId;
  activeSelectionFingerprintRef.current = selectionFingerprint;

  const invalidateCalculation = useCallback(() => {
    calculationGenerationRef.current += 1;
    calculationAbortControllerRef.current?.abort();
    calculationAbortControllerRef.current = null;
    calculationInFlightRef.current = false;
    setCalculationState({ status: 'idle', success: null });
    setSaveStatus('idle');
  }, []);

  const handleSelectionChange = useCallback(
    (nextSelection: PortionSelection) => {
      if (saveInFlightRef.current) {
        return;
      }

      invalidateCalculation();
      setSelection(nextSelection);
    },
    [invalidateCalculation],
  );

  const loadFood = useCallback(async (requestedFoodId: number) => {
    detailAbortControllerRef.current?.abort();

    const requestGeneration = detailGenerationRef.current + 1;
    detailGenerationRef.current = requestGeneration;
    const controller = new AbortController();
    detailAbortControllerRef.current = controller;

    setRequestState({ status: 'loading', foodId: requestedFoodId, food: null });

    try {
      const food = await getFood(requestedFoodId, controller.signal);

      if (
        controller.signal.aborted ||
        requestGeneration !== detailGenerationRef.current ||
        activeFoodIdRef.current !== requestedFoodId
      ) {
        return;
      }

      setRequestState({ status: 'success', foodId: requestedFoodId, food });
    } catch (error) {
      if (
        isAbortError(error) ||
        controller.signal.aborted ||
        requestGeneration !== detailGenerationRef.current ||
        activeFoodIdRef.current !== requestedFoodId
      ) {
        return;
      }

      setRequestState({ status: 'error', foodId: requestedFoodId, food: null });
    } finally {
      if (detailAbortControllerRef.current === controller) {
        detailAbortControllerRef.current = null;
      }
    }
  }, []);

  useEffect(() => {
    detailGenerationRef.current += 1;
    detailAbortControllerRef.current?.abort();
    detailAbortControllerRef.current = null;
    invalidateCalculation();
    setSelection({ kind: 'none' });

    if (foodId === null) {
      setRequestState({ status: 'invalid', foodId: null, food: null });
      return;
    }

    void loadFood(foodId);

    return () => {
      detailGenerationRef.current += 1;
      detailAbortControllerRef.current?.abort();
      detailAbortControllerRef.current = null;
    };
  }, [foodId, invalidateCalculation, loadFood]);

  useEffect(
    () => () => {
      calculationGenerationRef.current += 1;
      calculationAbortControllerRef.current?.abort();
      calculationAbortControllerRef.current = null;
      calculationInFlightRef.current = false;
    },
    [],
  );

  const displayedState: DetailRequestState =
    requestState.foodId === foodId
      ? requestState
      : {
          status: foodId === null ? 'invalid' : 'loading',
          foodId,
          food: null,
        };

  if (displayedState.status === 'invalid') {
    return (
      <View style={styles.centeredState}>
        <Text style={styles.errorTitle}>Geçersiz yiyecek kimliği.</Text>
        <Text style={styles.stateText}>Bu yiyecek detayı açılamıyor.</Text>
      </View>
    );
  }

  if (displayedState.status === 'loading') {
    return (
      <View style={styles.centeredState}>
        <ActivityIndicator color="#28785f" />
        <Text style={styles.stateText}>Yiyecek bilgileri yükleniyor…</Text>
      </View>
    );
  }

  if (displayedState.status === 'error') {
    return (
      <View style={styles.centeredState}>
        <Text style={styles.errorTitle}>Yiyecek bilgileri yüklenemedi.</Text>
        <Pressable
          accessibilityRole="button"
          onPress={() => {
            if (foodId !== null) {
              void loadFood(foodId);
            }
          }}
          style={styles.secondaryButton}
        >
          <Text style={styles.secondaryButtonText}>Tekrar Dene</Text>
        </Pressable>
      </View>
    );
  }

  const food = displayedState.food;

  if (food === null) {
    return null;
  }

  const calculationCandidate = buildCalculationCandidate(food, selection, selectionFingerprint);
  const freshCalculation =
    calculationState.status === 'success' &&
    calculationState.success.fingerprint === selectionFingerprint &&
    calculationState.success.result.foodId === food.foodId
      ? calculationState.success
      : null;
  const calculationIsRunning = calculationState.status === 'calculating';
  const saveIsRunning = saveStatus === 'saving';
  const calculateIsEnabled =
    calculationCandidate !== null &&
    !calculationIsRunning &&
    !saveIsRunning &&
    freshCalculation === null;

  const handleCalculate = async () => {
    if (
      calculationCandidate === null ||
      calculationInFlightRef.current ||
      saveInFlightRef.current
    ) {
      return;
    }

    calculationAbortControllerRef.current?.abort();
    const requestGeneration = calculationGenerationRef.current + 1;
    calculationGenerationRef.current = requestGeneration;
    const controller = new AbortController();
    calculationAbortControllerRef.current = controller;
    calculationInFlightRef.current = true;
    setCalculationState({ status: 'calculating', success: null });
    setSaveStatus('idle');

    try {
      const result = await calculateNutrition(calculationCandidate.input, controller.signal);

      if (
        controller.signal.aborted ||
        requestGeneration !== calculationGenerationRef.current ||
        activeFoodIdRef.current !== food.foodId ||
        activeSelectionFingerprintRef.current !== calculationCandidate.fingerprint
      ) {
        return;
      }

      setCalculationState({
        status: 'success',
        success: {
          fingerprint: calculationCandidate.fingerprint,
          result,
          selection: calculationCandidate.selection,
        },
      });
    } catch (error) {
      if (
        isAbortError(error) ||
        controller.signal.aborted ||
        requestGeneration !== calculationGenerationRef.current ||
        activeFoodIdRef.current !== food.foodId ||
        activeSelectionFingerprintRef.current !== calculationCandidate.fingerprint
      ) {
        return;
      }

      setCalculationState({ status: 'error', success: null });
    } finally {
      if (calculationAbortControllerRef.current === controller) {
        calculationAbortControllerRef.current = null;
        calculationInFlightRef.current = false;
      }
    }
  };

  const handleAddMeal = async () => {
    if (
      freshCalculation === null ||
      hydrationStatus !== 'ready' ||
      saveInFlightRef.current
    ) {
      return;
    }

    const mealItem = mapManualCalculationToMealItem(food, freshCalculation);
    const mealToSave = createLocalMealRecord([mealItem]);
    saveInFlightRef.current = true;
    setSaveStatus('saving');

    try {
      await addMeal(mealToSave);
      router.dismissTo('/(tabs)');
    } catch {
      setSaveStatus('error');
    } finally {
      saveInFlightRef.current = false;
    }
  };

  return (
    <ScrollView
      contentContainerStyle={styles.content}
      keyboardShouldPersistTaps="handled"
      style={styles.screen}
    >
      <View>
        <Text style={styles.title}>{food.displayName}</Text>
        {food.canonicalName !== food.displayName ? (
          <Text style={styles.canonicalName}>{food.canonicalName}</Text>
        ) : null}
        {food.brand !== null ? <Text style={styles.brand}>{food.brand}</Text> : null}
      </View>

      <NutritionReference nutrition={food.nutritionPer100g} />
      <PortionSelector
        disabled={saveIsRunning}
        isSelectionValid={calculationCandidate !== null}
        onSelectionChange={handleSelectionChange}
        portions={food.portions}
        selection={selection}
      />

      <Pressable
        accessibilityRole="button"
        disabled={!calculateIsEnabled}
        onPress={() => void handleCalculate()}
        style={({ pressed }) => [
          styles.primaryButton,
          !calculateIsEnabled && styles.disabledButton,
          pressed && calculateIsEnabled && styles.pressedButton,
        ]}
      >
        {calculationIsRunning ? <ActivityIndicator color="#ffffff" /> : null}
        <Text style={styles.primaryButtonText}>
          {calculationIsRunning ? 'Hesaplanıyor…' : 'Besin Değerini Hesapla'}
        </Text>
      </Pressable>

      {calculationState.status === 'error' ? (
        <Text style={styles.inlineError}>
          Besin değeri hesaplanamadı. Seçiminizi koruduk; tekrar deneyebilirsiniz.
        </Text>
      ) : null}

      {freshCalculation !== null ? (
        <>
          <CalculatedNutritionCard result={freshCalculation.result} />

          {hydrationStatus === 'ready' ? (
            <Pressable
              accessibilityRole="button"
              disabled={saveIsRunning}
              onPress={() => void handleAddMeal()}
              style={({ pressed }) => [
                styles.addButton,
                saveIsRunning && styles.disabledButton,
                pressed && !saveIsRunning && styles.pressedButton,
              ]}
            >
              {saveIsRunning ? <ActivityIndicator color="#ffffff" /> : null}
              <Text style={styles.primaryButtonText}>
                {saveIsRunning ? 'Günlüğe Ekleniyor…' : 'Günlüğe Ekle'}
              </Text>
            </Pressable>
          ) : (
            <Text style={styles.storeStatusText}>
              {hydrationStatus === 'hydrating'
                ? 'Öğün kayıtları hazırlanıyor…'
                : 'Öğün kaydı şu anda kullanılamıyor.'}
            </Text>
          )}

          {saveStatus === 'error' ? (
            <Text style={styles.inlineError}>
              Öğün günlüğe eklenemedi. Hesaplamanız korundu; tekrar deneyebilirsiniz.
            </Text>
          ) : null}
        </>
      ) : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#f7faf8' },
  content: { gap: 20, padding: 24, paddingBottom: 44 },
  centeredState: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 14,
    padding: 28,
    backgroundColor: '#f7faf8',
  },
  stateText: { color: '#64716c', fontSize: 15, lineHeight: 22, textAlign: 'center' },
  errorTitle: { color: '#7a3028', fontSize: 17, fontWeight: '700', textAlign: 'center' },
  secondaryButton: {
    borderRadius: 10,
    paddingHorizontal: 15,
    paddingVertical: 11,
    backgroundColor: '#e6f2ed',
  },
  secondaryButtonText: { color: '#1f664f', fontWeight: '700' },
  title: { color: '#1d2b26', fontSize: 30, fontWeight: '700' },
  canonicalName: { marginTop: 7, color: '#64716c', fontSize: 15 },
  brand: { marginTop: 8, color: '#28785f', fontSize: 15, fontWeight: '600' },
  primaryButton: {
    minHeight: 52,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 10,
    borderRadius: 14,
    paddingHorizontal: 18,
    paddingVertical: 15,
    backgroundColor: '#28785f',
  },
  addButton: {
    minHeight: 52,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 10,
    borderRadius: 14,
    paddingHorizontal: 18,
    paddingVertical: 15,
    backgroundColor: '#1f664f',
  },
  primaryButtonText: { color: '#ffffff', fontSize: 16, fontWeight: '700' },
  disabledButton: { opacity: 0.48 },
  pressedButton: { opacity: 0.8 },
  inlineError: { color: '#8e3b32', fontSize: 14, lineHeight: 20 },
  storeStatusText: { color: '#64716c', fontSize: 14, lineHeight: 20, textAlign: 'center' },
});
