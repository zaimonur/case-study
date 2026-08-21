import { StyleSheet, Text, View } from 'react-native';

import type {
  AggregateMetric,
  DailyNutritionAggregate,
} from '../../domain/dailyTotals';

const numberFormatter = new Intl.NumberFormat('tr-TR', {
  maximumFractionDigits: 2,
});

function formatMetric(metric: AggregateMetric, unit: string): string {
  if (metric.knownItemCount === 0 && metric.unknownItemCount > 0) {
    return '—';
  }

  const formattedTotal = `${numberFormatter.format(metric.knownTotal)} ${unit}`;
  return metric.unknownItemCount > 0 ? `En az ${formattedTotal}` : formattedTotal;
}

export function DailySummaryCard({ aggregate }: { aggregate: DailyNutritionAggregate }) {
  const entries = [
    { label: 'Kalori', value: formatMetric(aggregate.caloriesKcal, 'kcal') },
    { label: 'Protein', value: formatMetric(aggregate.proteinG, 'g') },
    { label: 'Karbonhidrat', value: formatMetric(aggregate.carbohydratesG, 'g') },
    { label: 'Yağ', value: formatMetric(aggregate.fatG, 'g') },
  ];
  const hasUnknownNutrition = Object.values(aggregate).some(
    (metric) => metric.unknownItemCount > 0,
  );

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
      {hasUnknownNutrition ? (
        <Text style={styles.infoNote}>
          Bazı öğünlerde besin verisi eksik. “En az” değerleri yalnızca bilinen verilerin
          toplamıdır.
        </Text>
      ) : null}
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
  infoNote: { marginTop: 15, color: '#60706a', fontSize: 13, lineHeight: 19 },
});
