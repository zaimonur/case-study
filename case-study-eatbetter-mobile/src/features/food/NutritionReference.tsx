import { StyleSheet, Text, View } from 'react-native';

import type { NutritionValues } from '../../domain/nutrition';

function formatNutritionValue(value: number | null, unit: string): string {
  return value === null ? '—' : `${value} ${unit}`;
}

export function NutritionReference({ nutrition }: { nutrition: NutritionValues }) {
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

const styles = StyleSheet.create({
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
});
