import { StyleSheet, Text, View } from 'react-native';

import type { NutritionValues } from '../../domain/nutrition';

const numberFormatter = new Intl.NumberFormat('tr-TR', {
  maximumFractionDigits: 2,
});

function formatValue(value: number | null, unit: string): string {
  return value === null ? '—' : `${numberFormatter.format(value)} ${unit}`;
}

export function DailySummaryCard({ totals }: { totals: NutritionValues }) {
  const entries = [
    { label: 'Kalori', value: formatValue(totals.caloriesKcal, 'kcal') },
    { label: 'Protein', value: formatValue(totals.proteinG, 'g') },
    { label: 'Karbonhidrat', value: formatValue(totals.carbohydratesG, 'g') },
    { label: 'Yağ', value: formatValue(totals.fatG, 'g') },
  ];

  return (
    <View style={styles.card}>
      <Text style={styles.title}>Günlük özet</Text>
      <View style={styles.grid}>
        {entries.map((entry) => (
          <View key={entry.label} style={styles.metric}>
            <Text style={styles.label}>{entry.label}</Text>
            <Text style={styles.value}>{entry.value}</Text>
          </View>
        ))}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    borderWidth: 1,
    borderColor: '#d4e4dc',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  title: { color: '#1d2b26', fontSize: 19, fontWeight: '700' },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12, marginTop: 16 },
  metric: { width: '47%', borderRadius: 12, padding: 13, backgroundColor: '#f1f7f4' },
  label: { color: '#64716c', fontSize: 13 },
  value: { marginTop: 5, color: '#1d2b26', fontSize: 17, fontWeight: '700' },
});
