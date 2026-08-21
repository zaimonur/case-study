import { StyleSheet, Text, View } from 'react-native';

import type {
  AnalysisAverageMetric,
  AnalysisAverageNutrition,
  NutritionCoverage,
  NutritionCoverageMetric,
  SevenDayAnalysis,
  SevenDayAnalysisDay,
} from '../../domain/analysis';
import type {
  AggregateMetric,
  DailyNutritionAggregate,
} from '../../domain/dailyTotals';

type NutrientKey = keyof DailyNutritionAggregate;

const NUTRIENTS: Array<{
  key: NutrientKey;
  label: string;
  unit: string;
}> = [
  { key: 'caloriesKcal', label: 'Kalori', unit: 'kcal' },
  { key: 'proteinG', label: 'Protein', unit: 'g' },
  { key: 'carbohydratesG', label: 'Karbonhidrat', unit: 'g' },
  { key: 'fatG', label: 'Yağ', unit: 'g' },
];

const numberFormatter = new Intl.NumberFormat('tr-TR', {
  maximumFractionDigits: 2,
});
const percentageFormatter = new Intl.NumberFormat('tr-TR', {
  maximumFractionDigits: 0,
  style: 'percent',
});
const weekdayFormatter = new Intl.DateTimeFormat('tr-TR', {
  weekday: 'short',
});
const dayFormatter = new Intl.DateTimeFormat('tr-TR', {
  day: 'numeric',
});

function formatKnownValue(value: number, unit: string): string {
  return `${numberFormatter.format(value)} ${unit}`;
}

function formatDailyMetric(metric: AggregateMetric, unit: string): string {
  if (metric.knownItemCount === 0) {
    return '—';
  }

  const value = formatKnownValue(metric.knownTotal, unit);
  return metric.unknownItemCount > 0 ? `En az ${value}` : value;
}

function formatAverageMetric(
  metric: AnalysisAverageMetric,
  unit: string,
): string {
  if (metric.status === 'unavailable') {
    return '—';
  }

  if (metric.status === 'no-data' || metric.value === null) {
    return 'Veri yok';
  }

  const value = formatKnownValue(metric.value, unit);
  return metric.status === 'partial' ? `En az ${value}` : value;
}

function formatTrendDate(date: Date): string {
  return `${weekdayFormatter.format(date)} ${dayFormatter.format(date)}`;
}

function formatTrendCalories(day: SevenDayAnalysisDay): string {
  if (!day.hasLoggedData) {
    return 'Kayıt yok';
  }

  const calories = day.nutrition.caloriesKcal;

  if (calories.knownItemCount === 0) {
    return 'Kalori bilinmiyor';
  }

  const value = formatKnownValue(calories.knownTotal, 'kcal');
  return calories.unknownItemCount > 0 ? `En az ${value}` : value;
}

function formatCoverage(metric: NutritionCoverageMetric): string {
  const totalItemCount = metric.knownItemCount + metric.unknownItemCount;

  if (totalItemCount === 0) {
    return 'Veri yok';
  }

  return percentageFormatter.format(metric.knownItemCount / totalItemCount);
}

function CardTitle({ children }: { children: string }) {
  return <Text style={styles.cardTitle}>{children}</Text>;
}

export function TodayNutritionCard({ day }: { day: SevenDayAnalysisDay }) {
  return (
    <View style={styles.card}>
      <CardTitle>Bugünün özeti</CardTitle>
      {!day.hasLoggedData ? (
        <View style={styles.emptyState}>
          <Text style={styles.emptyStateText}>Bugün henüz öğün kaydı yok.</Text>
        </View>
      ) : (
        <View style={styles.metricGrid}>
          {NUTRIENTS.map(({ key, label, unit }) => (
            <View key={key} style={styles.metricTile}>
              <Text style={styles.metricLabel}>{label}</Text>
              <Text style={styles.metricValue}>
                {formatDailyMetric(day.nutrition[key], unit)}
              </Text>
            </View>
          ))}
        </View>
      )}
    </View>
  );
}

export function AnalysisOverviewCard({
  analysis,
}: {
  analysis: SevenDayAnalysis;
}) {
  const entries = [
    { label: 'Kayıtlı gün', value: `${analysis.loggedDayCount} / 7` },
    { label: 'Öğün', value: numberFormatter.format(analysis.mealCount) },
    { label: 'Yiyecek', value: numberFormatter.format(analysis.itemCount) },
  ];

  return (
    <View style={styles.card}>
      <CardTitle>Son 7 gün</CardTitle>
      <View style={styles.overviewRow}>
        {entries.map((entry) => (
          <View key={entry.label} style={styles.overviewMetric}>
            <Text style={styles.metricLabel}>{entry.label}</Text>
            <Text style={styles.overviewValue}>{entry.value}</Text>
          </View>
        ))}
      </View>
    </View>
  );
}

export function AverageNutritionCard({
  averageNutrition,
}: {
  averageNutrition: AnalysisAverageNutrition;
}) {
  const hasPartialMetric = Object.values(averageNutrition).some(
    (metric) => metric.status === 'partial',
  );

  return (
    <View style={styles.card}>
      <CardTitle>Kayıtlı gün ortalaması</CardTitle>
      <View style={styles.metricGrid}>
        {NUTRIENTS.map(({ key, label, unit }) => (
          <View key={key} style={styles.metricTile}>
            <Text style={styles.metricLabel}>{label}</Text>
            <Text style={styles.metricValue}>
              {formatAverageMetric(averageNutrition[key], unit)}
            </Text>
          </View>
        ))}
      </View>
      {hasPartialMetric ? (
        <Text style={styles.infoNote}>
          “En az” değerleri yalnızca mevcut besin verilerine dayanır.
        </Text>
      ) : null}
    </View>
  );
}

export function CalorieTrendCard({ days }: { days: SevenDayAnalysisDay[] }) {
  const maximumKnownCalories = days.reduce((maximum, day) => {
    const calories = day.nutrition.caloriesKcal;

    return calories.knownItemCount > 0
      ? Math.max(maximum, calories.knownTotal)
      : maximum;
  }, 0);

  return (
    <View style={styles.card}>
      <CardTitle>7 günlük kalori görünümü</CardTitle>
      <View style={styles.trendList}>
        {days.map((day) => {
          const calories = day.nutrition.caloriesKcal;
          const hasKnownCalories = calories.knownItemCount > 0;
          const ratio =
            hasKnownCalories && maximumKnownCalories > 0
              ? Math.min(
                  Math.max(calories.knownTotal / maximumKnownCalories, 0),
                  1,
                )
              : 0;

          return (
            <View key={day.localDateKey} style={styles.trendRow}>
              <Text style={styles.trendDate}>{formatTrendDate(day.date)}</Text>
              <View style={styles.trendContent}>
                <Text
                  style={[
                    styles.trendValue,
                    !day.hasLoggedData && styles.unloggedText,
                  ]}
                >
                  {formatTrendCalories(day)}
                </Text>
                {hasKnownCalories ? (
                  <View style={styles.barTrack}>
                    <View style={[styles.barFill, { width: `${ratio * 100}%` }]} />
                  </View>
                ) : null}
              </View>
            </View>
          );
        })}
      </View>
    </View>
  );
}

export function NutritionCoverageCard({
  coverage,
}: {
  coverage: NutritionCoverage;
}) {
  return (
    <View style={styles.card}>
      <CardTitle>Veri kapsamı</CardTitle>
      <View style={styles.coverageList}>
        {NUTRIENTS.map(({ key, label }) => (
          <View key={key} style={styles.coverageRow}>
            <Text style={styles.coverageLabel}>{label}</Text>
            <Text style={styles.coverageValue}>{formatCoverage(coverage[key])}</Text>
          </View>
        ))}
      </View>
      <Text style={styles.infoNote}>
        Bu oranlar kayıtlı yiyeceklerde ilgili besin değerinin mevcut olma oranını
        gösterir.
      </Text>
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
  cardTitle: { color: '#1d2b26', fontSize: 19, fontWeight: '700' },
  emptyState: {
    marginTop: 16,
    borderRadius: 12,
    padding: 16,
    backgroundColor: '#f1f7f4',
  },
  emptyStateText: { color: '#52605b', fontSize: 15, lineHeight: 22 },
  metricGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 12,
    marginTop: 16,
  },
  metricTile: {
    minWidth: 120,
    flexBasis: '46%',
    flexGrow: 1,
    borderRadius: 12,
    padding: 13,
    backgroundColor: '#f1f7f4',
  },
  metricLabel: { color: '#64716c', fontSize: 13 },
  metricValue: {
    marginTop: 5,
    color: '#1d2b26',
    fontSize: 16,
    fontWeight: '700',
  },
  overviewRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 10,
    marginTop: 16,
  },
  overviewMetric: {
    minWidth: 84,
    flexBasis: '28%',
    flexGrow: 1,
    borderRadius: 12,
    padding: 12,
    backgroundColor: '#f1f7f4',
  },
  overviewValue: {
    marginTop: 6,
    color: '#1f664f',
    fontSize: 20,
    fontWeight: '700',
  },
  infoNote: { marginTop: 15, color: '#60706a', fontSize: 13, lineHeight: 19 },
  trendList: { gap: 16, marginTop: 18 },
  trendRow: { flexDirection: 'row', alignItems: 'flex-start', gap: 12 },
  trendDate: {
    width: 54,
    color: '#405049',
    fontSize: 14,
    fontWeight: '700',
    textTransform: 'capitalize',
  },
  trendContent: { flex: 1, minWidth: 0 },
  trendValue: { color: '#263832', fontSize: 14, fontWeight: '600' },
  unloggedText: { color: '#7a8782', fontWeight: '400' },
  barTrack: {
    height: 8,
    marginTop: 7,
    overflow: 'hidden',
    borderRadius: 999,
    backgroundColor: '#e4eee9',
  },
  barFill: { height: '100%', borderRadius: 999, backgroundColor: '#43a37e' },
  coverageList: { gap: 13, marginTop: 17 },
  coverageRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 16,
  },
  coverageLabel: { flex: 1, color: '#52605b', fontSize: 15 },
  coverageValue: { color: '#1d2b26', fontSize: 15, fontWeight: '700' },
});
