import type { AnalysisAverageMetric } from '../../domain/analysis';
import type { AggregateMetric } from '../../domain/dailyTotals';
import type {
  ReportLocalDate,
  ReportLocalDateTime,
} from '../../domain/nutritionReport';

const TURKISH_MONTHS = [
  'Ocak',
  'Şubat',
  'Mart',
  'Nisan',
  'Mayıs',
  'Haziran',
  'Temmuz',
  'Ağustos',
  'Eylül',
  'Ekim',
  'Kasım',
  'Aralık',
] as const;

const TURKISH_WEEKDAYS = [
  'Pazar',
  'Pazartesi',
  'Salı',
  'Çarşamba',
  'Perşembe',
  'Cuma',
  'Cumartesi',
] as const;

const reportNumberFormatter = new Intl.NumberFormat('tr-TR', {
  minimumFractionDigits: 0,
  maximumFractionDigits: 1,
});

export function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => {
    switch (character) {
      case '&':
        return '&amp;';
      case '<':
        return '&lt;';
      case '>':
        return '&gt;';
      case '"':
        return '&quot;';
      case "'":
        return '&#39;';
      default:
        return character;
    }
  });
}

export function formatReportNumber(value: number): string {
  return reportNumberFormatter.format(value);
}

export function formatReportDate(date: ReportLocalDate): string {
  return `${date.day} ${TURKISH_MONTHS[date.month - 1]} ${date.year}`;
}

export function formatReportDateWithWeekday(date: ReportLocalDate): string {
  return `${TURKISH_WEEKDAYS[date.weekday]}, ${formatReportDate(date)}`;
}

export function formatReportTime(dateTime: ReportLocalDateTime): string {
  return `${String(dateTime.hour).padStart(2, '0')}:${String(dateTime.minute).padStart(2, '0')}`;
}

export function formatReportDateTime(dateTime: ReportLocalDateTime): string {
  return `${formatReportDate(dateTime)} ${formatReportTime(dateTime)}`;
}

export function formatAggregateMetric(
  metric: AggregateMetric,
  unit: string,
): string {
  if (metric.knownItemCount === 0) {
    return metric.unknownItemCount > 0 ? 'Bilinmiyor' : 'Veri yok';
  }

  const value = `${formatReportNumber(metric.knownTotal)} ${unit}`;

  return metric.unknownItemCount > 0 ? `${value} — kısmi` : value;
}

export function formatAverageMetric(
  metric: AnalysisAverageMetric,
  unit: string,
): string {
  if (metric.status === 'no-data') {
    return 'Veri yok';
  }

  if (metric.status === 'unavailable' || metric.value === null) {
    return 'Bilinmiyor';
  }

  const value = `${formatReportNumber(metric.value)} ${unit}`;

  return metric.status === 'partial' ? `${value} — kısmi` : value;
}

export function formatNullableNutritionValue(
  value: number | null,
  unit: string,
): string {
  return value === null
    ? 'Bilinmiyor'
    : `${formatReportNumber(value)} ${unit}`;
}
