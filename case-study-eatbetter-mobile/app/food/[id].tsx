import { useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { isAbortError } from '../../src/api/client';
import { getFood } from '../../src/api/foods';
import type { FoodDetail, FoodPortion } from '../../src/domain/food';
import type { NutritionValues } from '../../src/domain/nutrition';

type DetailRequestState = {
  status: 'invalid' | 'loading' | 'success' | 'error';
  foodId: number | null;
  food: FoodDetail | null;
};

type PortionSelection =
  | { kind: 'none' }
  | { kind: 'grams'; grams: string }
  | { kind: 'portion'; portionId: number; quantity: string };

function parseFoodId(id: string | string[] | undefined): number | null {
  const routeId = Array.isArray(id) ? id[0] : id;

  if (routeId === undefined || routeId.trim().length === 0) {
    return null;
  }

  const parsedFoodId = Number(routeId);
  return Number.isSafeInteger(parsedFoodId) && parsedFoodId > 0 ? parsedFoodId : null;
}

function isPositiveNumberInput(value: string): boolean {
  const trimmedValue = value.trim();

  if (trimmedValue.length === 0) {
    return false;
  }

  const parsedValue = Number(trimmedValue);
  return Number.isFinite(parsedValue) && parsedValue > 0;
}

function formatNutritionValue(value: number | null, unit: string): string {
  return value === null ? '—' : `${value} ${unit}`;
}

function NutritionReference({ nutrition }: { nutrition: NutritionValues }) {
  const rows = [
    { label: 'Kalori', value: formatNutritionValue(nutrition.caloriesKcal, 'kcal') },
    { label: 'Protein', value: formatNutritionValue(nutrition.proteinG, 'g') },
    { label: 'Karbonhidrat', value: formatNutritionValue(nutrition.carbohydratesG, 'g') },
    { label: 'Yağ', value: formatNutritionValue(nutrition.fatG, 'g') },
  ];

  return (
    <View style={styles.card}>
      <Text style={styles.sectionTitle}>100 g için referans</Text>
      <Text style={styles.referenceNote}>
        Bunlar seçtiğiniz miktarın değil, backend tarafından sağlanan 100 g değerleridir.
      </Text>
      <View style={styles.nutritionRows}>
        {rows.map((row) => (
          <View key={row.label} style={styles.nutritionRow}>
            <Text style={styles.nutritionLabel}>{row.label}</Text>
            <Text style={styles.nutritionValue}>{row.value}</Text>
          </View>
        ))}
      </View>
    </View>
  );
}

function PortionOption({
  isSelected,
  onPress,
  portion,
}: {
  isSelected: boolean;
  onPress: () => void;
  portion: FoodPortion;
}) {
  return (
    <Pressable
      accessibilityRole="radio"
      accessibilityState={{ checked: isSelected }}
      onPress={onPress}
      style={({ pressed }) => [
        styles.selectionOption,
        isSelected && styles.selectionOptionSelected,
        pressed && styles.selectionOptionPressed,
      ]}
    >
      <View style={[styles.radio, isSelected && styles.radioSelected]} />
      <Text style={styles.selectionOptionText}>
        {portion.amount} {portion.measure} · {portion.grams} g
      </Text>
    </Pressable>
  );
}

function AmountSelector({
  portions,
  selection,
  setSelection,
}: {
  portions: FoodPortion[];
  selection: PortionSelection;
  setSelection: (selection: PortionSelection) => void;
}) {
  const gramsAreValid = selection.kind === 'grams' && isPositiveNumberInput(selection.grams);
  const quantityIsValid =
    selection.kind === 'portion' && isPositiveNumberInput(selection.quantity);

  return (
    <View style={styles.card}>
      <Text style={styles.sectionTitle}>Miktar seçimi</Text>
      <Text style={styles.selectionNote}>Bir yöntem seçin. Herhangi bir miktar varsayılmaz.</Text>

      <Pressable
        accessibilityRole="radio"
        accessibilityState={{ checked: selection.kind === 'grams' }}
        onPress={() => {
          if (selection.kind !== 'grams') {
            setSelection({ kind: 'grams', grams: '' });
          }
        }}
        style={({ pressed }) => [
          styles.selectionOption,
          selection.kind === 'grams' && styles.selectionOptionSelected,
          pressed && styles.selectionOptionPressed,
        ]}
      >
        <View style={[styles.radio, selection.kind === 'grams' && styles.radioSelected]} />
        <Text style={styles.selectionOptionText}>Doğrudan gram gir</Text>
      </Pressable>

      {selection.kind === 'grams' ? (
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>Gram</Text>
          <TextInput
            keyboardType="decimal-pad"
            onChangeText={(grams) => setSelection({ kind: 'grams', grams })}
            placeholder="Örn. 150"
            style={styles.input}
            value={selection.grams}
          />
          {selection.grams.trim().length === 0 ? (
            <Text style={styles.inputHint}>0'dan büyük bir gram değeri girin.</Text>
          ) : (
            <Text style={gramsAreValid ? styles.validText : styles.invalidText}>
              {gramsAreValid ? 'Gram seçimi geçerli.' : 'Geçerli, pozitif bir gram değeri girin.'}
            </Text>
          )}
        </View>
      ) : null}

      {portions.length > 0 ? (
        <View style={styles.portionGroup}>
          <Text style={styles.inputLabel}>Kayıtlı porsiyonlar</Text>
          {portions.map((portion) => {
            const isSelected =
              selection.kind === 'portion' && selection.portionId === portion.portionId;

            return (
              <PortionOption
                isSelected={isSelected}
                key={portion.portionId}
                onPress={() => {
                  if (!isSelected) {
                    setSelection({ kind: 'portion', portionId: portion.portionId, quantity: '' });
                  }
                }}
                portion={portion}
              />
            );
          })}
        </View>
      ) : (
        <Text style={styles.noPortions}>Bu yiyecek için kayıtlı porsiyon bulunmuyor.</Text>
      )}

      {selection.kind === 'portion' ? (
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>Porsiyon adedi</Text>
          <TextInput
            keyboardType="decimal-pad"
            onChangeText={(quantity) =>
              setSelection({ kind: 'portion', portionId: selection.portionId, quantity })
            }
            placeholder="Örn. 2"
            style={styles.input}
            value={selection.quantity}
          />
          {selection.quantity.trim().length === 0 ? (
            <Text style={styles.inputHint}>0'dan büyük bir porsiyon adedi girin.</Text>
          ) : (
            <Text style={quantityIsValid ? styles.validText : styles.invalidText}>
              {quantityIsValid
                ? 'Porsiyon seçimi geçerli.'
                : 'Geçerli, pozitif bir porsiyon adedi girin.'}
            </Text>
          )}
        </View>
      ) : null}
    </View>
  );
}

export default function FoodDetailScreen() {
  const { id } = useLocalSearchParams<{ id?: string | string[] }>();
  const foodId = parseFoodId(id);
  const activeFoodIdRef = useRef(foodId);
  const requestGenerationRef = useRef(0);
  const abortControllerRef = useRef<AbortController | null>(null);
  const [requestState, setRequestState] = useState<DetailRequestState>({
    status: foodId === null ? 'invalid' : 'loading',
    foodId,
    food: null,
  });
  const [selection, setSelection] = useState<PortionSelection>({ kind: 'none' });

  activeFoodIdRef.current = foodId;

  const loadFood = useCallback(async (requestedFoodId: number) => {
    abortControllerRef.current?.abort();

    const requestGeneration = requestGenerationRef.current + 1;
    requestGenerationRef.current = requestGeneration;
    const controller = new AbortController();
    abortControllerRef.current = controller;

    setRequestState({ status: 'loading', foodId: requestedFoodId, food: null });

    try {
      const food = await getFood(requestedFoodId, controller.signal);

      if (
        controller.signal.aborted ||
        requestGeneration !== requestGenerationRef.current ||
        activeFoodIdRef.current !== requestedFoodId
      ) {
        return;
      }

      setRequestState({ status: 'success', foodId: requestedFoodId, food });
    } catch (error) {
      if (
        isAbortError(error) ||
        controller.signal.aborted ||
        requestGeneration !== requestGenerationRef.current ||
        activeFoodIdRef.current !== requestedFoodId
      ) {
        return;
      }

      setRequestState({ status: 'error', foodId: requestedFoodId, food: null });
    } finally {
      if (abortControllerRef.current === controller) {
        abortControllerRef.current = null;
      }
    }
  }, []);

  useEffect(() => {
    requestGenerationRef.current += 1;
    abortControllerRef.current?.abort();
    abortControllerRef.current = null;
    setSelection({ kind: 'none' });

    if (foodId === null) {
      setRequestState({ status: 'invalid', foodId: null, food: null });
      return;
    }

    void loadFood(foodId);

    return () => {
      requestGenerationRef.current += 1;
      abortControllerRef.current?.abort();
      abortControllerRef.current = null;
    };
  }, [foodId, loadFood]);

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
          style={styles.retryButton}
        >
          <Text style={styles.retryButtonText}>Tekrar Dene</Text>
        </Pressable>
      </View>
    );
  }

  const food = displayedState.food;

  if (food === null) {
    return null;
  }

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
      <AmountSelector portions={food.portions} selection={selection} setSelection={setSelection} />
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
  retryButton: {
    borderRadius: 10,
    paddingHorizontal: 15,
    paddingVertical: 11,
    backgroundColor: '#e6f2ed',
  },
  retryButtonText: { color: '#1f664f', fontWeight: '700' },
  title: { color: '#1d2b26', fontSize: 30, fontWeight: '700' },
  canonicalName: { marginTop: 7, color: '#64716c', fontSize: 15 },
  brand: { marginTop: 8, color: '#28785f', fontSize: 15, fontWeight: '600' },
  card: {
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  sectionTitle: { color: '#1d2b26', fontSize: 19, fontWeight: '700' },
  referenceNote: { marginTop: 7, color: '#6c7873', fontSize: 13, lineHeight: 19 },
  nutritionRows: { marginTop: 16, gap: 11 },
  nutritionRow: { flexDirection: 'row', justifyContent: 'space-between', gap: 16 },
  nutritionLabel: { color: '#52605b', fontSize: 15 },
  nutritionValue: { color: '#1d2b26', fontSize: 15, fontWeight: '600' },
  selectionNote: { marginTop: 7, marginBottom: 15, color: '#6c7873', fontSize: 13 },
  selectionOption: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    borderWidth: 1,
    borderColor: '#d5e1db',
    borderRadius: 12,
    padding: 14,
    backgroundColor: '#ffffff',
  },
  selectionOptionSelected: { borderColor: '#28785f', backgroundColor: '#eff7f3' },
  selectionOptionPressed: { opacity: 0.76 },
  selectionOptionText: { flex: 1, color: '#26332e', fontSize: 15, fontWeight: '600' },
  radio: { width: 18, height: 18, borderWidth: 2, borderColor: '#95a39d', borderRadius: 9 },
  radioSelected: { borderWidth: 5, borderColor: '#28785f' },
  inputGroup: { gap: 8, marginTop: 14 },
  portionGroup: { gap: 10, marginTop: 20 },
  inputLabel: { color: '#52605b', fontSize: 14, fontWeight: '700' },
  input: {
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 12,
    paddingHorizontal: 14,
    paddingVertical: 12,
    backgroundColor: '#ffffff',
    color: '#1d2b26',
    fontSize: 16,
  },
  inputHint: { color: '#6c7873', fontSize: 13 },
  validText: { color: '#28785f', fontSize: 13, fontWeight: '600' },
  invalidText: { color: '#9b3f34', fontSize: 13, fontWeight: '600' },
  noPortions: { marginTop: 18, color: '#6c7873', fontSize: 14, lineHeight: 20 },
});
