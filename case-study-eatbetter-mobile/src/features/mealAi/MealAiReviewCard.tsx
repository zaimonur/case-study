import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';

import type { MealItem, MealSelectionSnapshot } from '../../domain/meal';

type MealAiReviewCardProps = {
  hydrationStatus: 'hydrating' | 'ready' | 'error';
  items: MealItem[];
  onSave: () => Promise<void>;
  saveStatus: 'idle' | 'saving' | 'error';
};

function formatNutritionValue(value: number | null, unit: string): string {
  return value === null ? '—' : `${value} ${unit}`;
}

function formatSelection(selection: MealSelectionSnapshot): string {
  if (selection.kind === 'grams') {
    return `${selection.grams} g`;
  }

  return `${selection.quantity} × ${selection.amount} ${selection.measure}`;
}

export function MealAiReviewCard({
  hydrationStatus,
  items,
  onSave,
  saveStatus,
}: MealAiReviewCardProps) {
  const isSaving = saveStatus === 'saving';

  return (
    <View style={styles.review}>
      <View style={styles.header}>
        <Text style={styles.title}>Öğünü kontrol et</Text>
        <Text style={styles.description}>
          Kaydetmeden önce çözümlenen yiyecekleri kontrol et.
        </Text>
      </View>

      <View style={styles.items}>
        {items.map((item, itemIndex) => (
          <View key={`${item.foodId}-${itemIndex}`} style={styles.itemCard}>
            <Text style={styles.itemName}>{item.displayName}</Text>
            {item.brand !== null ? <Text style={styles.brand}>{item.brand}</Text> : null}

            <View style={styles.rows}>
              <View style={styles.row}>
                <Text style={styles.label}>Çözümlenen miktar</Text>
                <Text style={styles.value}>{item.resolvedGrams} g</Text>
              </View>
              <View style={styles.row}>
                <Text style={styles.label}>Seçim</Text>
                <Text style={styles.value}>{formatSelection(item.selection)}</Text>
              </View>
              {item.selection.kind === 'portion' ? (
                <View style={styles.row}>
                  <Text style={styles.label}>Porsiyon ağırlığı</Text>
                  <Text style={styles.value}>{item.selection.portionGrams} g</Text>
                </View>
              ) : null}
              <View style={styles.row}>
                <Text style={styles.label}>Kalori</Text>
                <Text style={styles.value}>
                  {formatNutritionValue(item.nutrition.caloriesKcal, 'kcal')}
                </Text>
              </View>
              <View style={styles.row}>
                <Text style={styles.label}>Protein</Text>
                <Text style={styles.value}>
                  {formatNutritionValue(item.nutrition.proteinG, 'g')}
                </Text>
              </View>
              <View style={styles.row}>
                <Text style={styles.label}>Karbonhidrat</Text>
                <Text style={styles.value}>
                  {formatNutritionValue(item.nutrition.carbohydratesG, 'g')}
                </Text>
              </View>
              <View style={styles.row}>
                <Text style={styles.label}>Yağ</Text>
                <Text style={styles.value}>
                  {formatNutritionValue(item.nutrition.fatG, 'g')}
                </Text>
              </View>
            </View>
          </View>
        ))}
      </View>

      {hydrationStatus === 'ready' ? (
        <Pressable
          accessibilityRole="button"
          accessibilityState={{ disabled: isSaving }}
          disabled={isSaving}
          onPress={() => void onSave()}
          style={({ pressed }) => [
            styles.saveButton,
            isSaving && styles.saveButtonDisabled,
            pressed && !isSaving && styles.saveButtonPressed,
          ]}
        >
          {isSaving ? <ActivityIndicator color="#ffffff" /> : null}
          <Text style={styles.saveButtonText}>
            {isSaving ? 'Günlüğe ekleniyor…' : 'Günlüğe Ekle'}
          </Text>
        </Pressable>
      ) : (
        <Text accessibilityLiveRegion="polite" style={styles.storeStatusText}>
          {hydrationStatus === 'hydrating'
            ? 'Öğün kayıtları hazırlanıyor…'
            : 'Öğün kaydı şu anda kullanılamıyor.'}
        </Text>
      )}

      {saveStatus === 'error' ? (
        <Text accessibilityLiveRegion="polite" style={styles.errorText}>
          Öğün günlüğe eklenemedi. Tekrar deneyebilirsin.
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  review: { gap: 14 },
  header: { gap: 5 },
  title: { color: '#1d2b26', fontSize: 19, fontWeight: '700' },
  description: { color: '#52605b', fontSize: 15, lineHeight: 22 },
  items: { gap: 12 },
  itemCard: {
    borderWidth: 1,
    borderColor: '#b9d8ca',
    borderRadius: 14,
    padding: 15,
    backgroundColor: '#eff7f3',
  },
  itemName: { color: '#194c3c', fontSize: 17, fontWeight: '700' },
  brand: { marginTop: 5, color: '#28785f', fontSize: 14, fontWeight: '600' },
  rows: { gap: 9, marginTop: 14 },
  row: { flexDirection: 'row', justifyContent: 'space-between', gap: 16 },
  label: { flex: 1, color: '#52605b', fontSize: 14 },
  value: { color: '#1d2b26', fontSize: 14, fontWeight: '700', textAlign: 'right' },
  saveButton: {
    minHeight: 52,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 10,
    borderRadius: 14,
    paddingHorizontal: 18,
    backgroundColor: '#1f664f',
  },
  saveButtonDisabled: { opacity: 0.48 },
  saveButtonPressed: { opacity: 0.8 },
  saveButtonText: { color: '#ffffff', fontSize: 16, fontWeight: '700' },
  storeStatusText: { color: '#64716c', fontSize: 14, lineHeight: 20, textAlign: 'center' },
  errorText: { color: '#8e3b32', fontSize: 14, lineHeight: 20 },
});
