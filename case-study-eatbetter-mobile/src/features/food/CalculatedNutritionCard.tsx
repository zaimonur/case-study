import { StyleSheet, Text, View } from 'react-native';

import type { CalculatedNutrition } from '../../domain/nutrition';

function formatNutritionValue(value: number | null, unit: string): string {
  return value === null ? '—' : `${value} ${unit}`;
}

export function CalculatedNutritionCard({ result }: { result: CalculatedNutrition }) {
  const rows = [
    { label: 'Çözümlenen gram', value: `${result.resolvedGrams} g` },
    { label: 'Kalori', value: formatNutritionValue(result.nutrition.caloriesKcal, 'kcal') },
    { label: 'Protein', value: formatNutritionValue(result.nutrition.proteinG, 'g') },
    {
      label: 'Karbonhidrat',
      value: formatNutritionValue(result.nutrition.carbohydratesG, 'g'),
    },
    { label: 'Yağ', value: formatNutritionValue(result.nutrition.fatG, 'g') },
  ];

  return (
    <View style={styles.card}>
      <Text style={styles.title}>Hesaplanan besin değeri</Text>
      <Text style={styles.note}>Bu değerler backend hesaplama sonucudur.</Text>
      <View style={styles.rows}>
        {rows.map((row) => (
          <View key={row.label} style={styles.row}>
            <Text style={styles.label}>{row.label}</Text>
            <Text style={styles.value}>{row.value}</Text>
          </View>
        ))}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    borderWidth: 1,
    borderColor: '#b9d8ca',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#eff7f3',
  },
  title: { color: '#194c3c', fontSize: 19, fontWeight: '700' },
  note: { marginTop: 7, color: '#527064', fontSize: 13 },
  rows: { marginTop: 16, gap: 11 },
  row: { flexDirection: 'row', justifyContent: 'space-between', gap: 16 },
  label: { color: '#52605b', fontSize: 15 },
  value: { color: '#1d2b26', fontSize: 15, fontWeight: '700' },
});
