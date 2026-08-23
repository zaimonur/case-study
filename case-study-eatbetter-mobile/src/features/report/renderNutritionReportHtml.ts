import type { DailyNutritionAggregate } from '../../domain/dailyTotals';
import type {
  NutritionReport,
  NutritionReportDay,
  NutritionReportItem,
  NutritionReportMeal,
} from '../../domain/nutritionReport';
import { formatPortionAmountAndMeasure } from '../food/formatPortionDescription';
import {
  escapeHtml,
  formatAggregateMetric,
  formatAverageMetric,
  formatNullableNutritionValue,
  formatReportDate,
  formatReportDateTime,
  formatReportDateWithWeekday,
  formatReportNumber,
  formatReportTime,
} from './reportFormatting';

type NutrientKey = keyof DailyNutritionAggregate;

const NUTRIENTS: ReadonlyArray<{
  readonly key: NutrientKey;
  readonly label: string;
  readonly unit: string;
}> = [
  { key: 'caloriesKcal', label: 'Kalori', unit: 'kcal' },
  { key: 'proteinG', label: 'Protein', unit: 'g' },
  { key: 'carbohydratesG', label: 'Karbonhidrat', unit: 'g' },
  { key: 'fatG', label: 'Yağ', unit: 'g' },
];

function h(value: string): string {
  return escapeHtml(value);
}

function renderAverageNutrition(report: NutritionReport): string {
  return NUTRIENTS.map(({ key, label, unit }) => `
    <div class="metric-card">
      <div class="metric-label">${label}</div>
      <div class="metric-value">${h(formatAverageMetric(report.summary.averageNutrition[key], unit))}</div>
    </div>`).join('');
}

function renderCoverage(report: NutritionReport): string {
  return NUTRIENTS.map(({ key, label }) => {
    const coverage = report.summary.nutritionCoverage[key];

    return `
      <tr>
        <th scope="row">${label}</th>
        <td>${h(formatReportNumber(coverage.knownItemCount))}</td>
        <td>${h(formatReportNumber(coverage.unknownItemCount))}</td>
      </tr>`;
  }).join('');
}

function renderDailyNutrition(day: NutritionReportDay): string {
  return NUTRIENTS.map(({ key, label, unit }) => `
    <div class="daily-nutrient">
      <span>${label}</span>
      <strong>${h(formatAggregateMetric(day.nutrition[key], unit))}</strong>
    </div>`).join('');
}

function renderDailySummary(days: readonly NutritionReportDay[]): string {
  return days.map((day) => `
    <article class="day-summary">
      <div class="day-summary-heading">
        <div>
          <h3>${h(formatReportDateWithWeekday(day.date))}</h3>
          <p>${day.hasLoggedData ? 'Kayıtlı gün' : 'Kayıt yok'}</p>
        </div>
        <div class="day-counts">
          ${h(formatReportNumber(day.mealCount))} öğün · ${h(formatReportNumber(day.itemCount))} yiyecek
        </div>
      </div>
      <div class="daily-grid">${renderDailyNutrition(day)}</div>
    </article>`).join('');
}

function formatSelection(item: NutritionReportItem): string {
  if (item.selection.kind === 'grams') {
    return `${formatReportNumber(item.selection.grams)} g`;
  }

  const amountAndMeasure = formatPortionAmountAndMeasure(
    item.selection.amount,
    item.selection.measure,
  );

  return item.selection.quantity === 1
    ? amountAndMeasure
    : `${formatReportNumber(item.selection.quantity)} × ${amountAndMeasure}`;
}

function renderItem(item: NutritionReportItem): string {
  const canonicalName =
    item.canonicalName !== item.displayName
      ? `<div class="food-canonical">${h(item.canonicalName)}</div>`
      : '';
  const brand =
    item.brand === null || item.brand.trim() === ''
      ? ''
      : `<div class="food-brand">${h(item.brand)}</div>`;

  return `
    <div class="food-item">
      <div class="food-heading">
        <div class="food-title-wrap">
          <div class="food-name">${h(item.displayName)}</div>
          ${canonicalName}
          ${brand}
        </div>
        <div class="food-amount">
          <span>Seçim: ${h(formatSelection(item))}</span>
          <span>Çözümlenen: ${h(formatReportNumber(item.resolvedGrams))} g</span>
        </div>
      </div>
      <div class="food-nutrition">
        <span>Kalori <strong>${h(formatNullableNutritionValue(item.nutrition.caloriesKcal, 'kcal'))}</strong></span>
        <span>Protein <strong>${h(formatNullableNutritionValue(item.nutrition.proteinG, 'g'))}</strong></span>
        <span>Karbonhidrat <strong>${h(formatNullableNutritionValue(item.nutrition.carbohydratesG, 'g'))}</strong></span>
        <span>Yağ <strong>${h(formatNullableNutritionValue(item.nutrition.fatG, 'g'))}</strong></span>
      </div>
    </div>`;
}

function renderMeal(meal: NutritionReportMeal): string {
  return `
    <section class="meal-block">
      <h4>Öğün · ${h(formatReportTime(meal.loggedAt))}</h4>
      ${meal.items.map(renderItem).join('')}
    </section>`;
}

function renderDetailedDay(day: NutritionReportDay): string {
  return `
    <article class="detail-day">
      <h3>${h(formatReportDateWithWeekday(day.date))}</h3>
      ${
        day.meals.length === 0
          ? '<p class="empty-note">Bu gün için kayıtlı öğün yok.</p>'
          : day.meals.map(renderMeal).join('')
      }
    </article>`;
}

export function renderNutritionReportHtml(report: NutritionReport): string {
  const period = `${formatReportDate(report.period.startDate)} – ${formatReportDate(report.period.endDate)}`;

  return `<!DOCTYPE html>
<html lang="tr">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>EatBetter Beslenme Raporu</title>
  <style>
    @page { margin: 16mm 14mm 18mm; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      color: #1d2b26;
      background: #ffffff;
      font-family: Arial, Helvetica, sans-serif;
      font-size: 10.5pt;
      line-height: 1.45;
      overflow-wrap: anywhere;
    }
    h1, h2, h3, h4, p { margin-top: 0; }
    h1 { margin-bottom: 6px; color: #1f664f; font-size: 24pt; }
    h2 { margin: 0 0 12px; font-size: 15pt; }
    h3 { margin-bottom: 4px; font-size: 12.5pt; }
    h4 { margin-bottom: 9px; color: #1f664f; font-size: 10.5pt; }
    .header { padding-bottom: 18px; border-bottom: 2px solid #28785f; }
    .header p { margin: 2px 0; color: #52605b; }
    .section { margin-top: 22px; }
    .overview, .metric-grid, .daily-grid, .food-nutrition {
      display: grid;
      gap: 9px;
    }
    .overview { grid-template-columns: repeat(3, 1fr); }
    .metric-grid { grid-template-columns: repeat(4, 1fr); }
    .overview-card, .metric-card {
      break-inside: avoid;
      border: 1px solid #d4e4dc;
      border-radius: 8px;
      padding: 11px;
      background: #f4f8f6;
    }
    .overview-label, .metric-label { color: #64716c; font-size: 8.5pt; }
    .overview-value, .metric-value { margin-top: 4px; font-weight: 700; }
    .overview-value { color: #1f664f; font-size: 16pt; }
    table { width: 100%; border-collapse: collapse; }
    th, td { border-bottom: 1px solid #dce7e1; padding: 8px; text-align: left; }
    thead th { color: #52605b; background: #f4f8f6; font-size: 8.5pt; }
    .day-summary {
      break-inside: avoid;
      margin-bottom: 10px;
      border: 1px solid #d4e4dc;
      border-radius: 8px;
      padding: 11px;
    }
    .day-summary-heading, .food-heading {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 14px;
    }
    .day-summary-heading p, .empty-note { margin-bottom: 0; color: #6b7772; }
    .day-counts { flex: 0 0 auto; color: #405049; font-size: 9pt; }
    .daily-grid { grid-template-columns: repeat(4, 1fr); margin-top: 9px; }
    .daily-nutrient { min-width: 0; }
    .daily-nutrient span { display: block; color: #64716c; font-size: 8pt; }
    .daily-nutrient strong { display: block; margin-top: 2px; font-size: 9pt; }
    .detail-day { margin: 0 0 18px; }
    .detail-day > h3 { padding-bottom: 6px; border-bottom: 1px solid #a9c9bb; }
    .meal-block {
      break-inside: avoid;
      margin-top: 10px;
      border-left: 3px solid #43a37e;
      padding: 4px 0 3px 11px;
    }
    .food-item { padding: 9px 0; border-top: 1px solid #e3ebe7; }
    .food-item:first-of-type { border-top: 0; }
    .food-title-wrap { min-width: 0; }
    .food-name { font-weight: 700; }
    .food-canonical, .food-brand, .food-amount { color: #64716c; font-size: 8.5pt; }
    .food-amount { display: flex; flex: 0 1 42%; flex-direction: column; text-align: right; }
    .food-nutrition { grid-template-columns: repeat(4, 1fr); margin-top: 7px; font-size: 8pt; }
    .food-nutrition span { min-width: 0; color: #64716c; }
    .food-nutrition strong { display: block; margin-top: 1px; color: #263832; }
    .footer {
      margin-top: 24px;
      border-top: 1px solid #d4e4dc;
      padding-top: 11px;
      color: #64716c;
      font-size: 8.5pt;
    }
    .footer p { margin-bottom: 4px; }
    @media print {
      .section { break-before: auto; }
      thead { display: table-header-group; }
    }
  </style>
</head>
<body>
  <header class="header">
    <h1>EatBetter Beslenme Raporu</h1>
    <p>Rapor dönemi: ${h(period)}</p>
    <p>Oluşturulma: ${h(formatReportDateTime(report.generatedAt))}</p>
  </header>

  <main>
    <section class="section">
      <h2>Yedi günlük genel görünüm</h2>
      <div class="overview">
        <div class="overview-card"><div class="overview-label">Kayıtlı gün</div><div class="overview-value">${h(formatReportNumber(report.summary.loggedDayCount))} / 7</div></div>
        <div class="overview-card"><div class="overview-label">Öğün</div><div class="overview-value">${h(formatReportNumber(report.summary.mealCount))}</div></div>
        <div class="overview-card"><div class="overview-label">Yiyecek</div><div class="overview-value">${h(formatReportNumber(report.summary.itemCount))}</div></div>
      </div>
    </section>

    <section class="section">
      <h2>Kayıtlı gün ortalaması</h2>
      <div class="metric-grid">${renderAverageNutrition(report)}</div>
    </section>

    <section class="section">
      <h2>Besin verisi kapsamı</h2>
      <table>
        <thead><tr><th>Besin</th><th>Bilinen yiyecek</th><th>Bilinmeyen yiyecek</th></tr></thead>
        <tbody>${renderCoverage(report)}</tbody>
      </table>
    </section>

    <section class="section">
      <h2>Günlük özet</h2>
      ${renderDailySummary(report.days)}
    </section>

    <section class="section">
      <h2>Ayrıntılı yiyecek günlüğü</h2>
      ${report.days.map(renderDetailedDay).join('')}
    </section>
  </main>

  <footer class="footer">
    <p>Bu rapor, EatBetter uygulamasında kayıtlı verilere dayanılarak oluşturulmuştur.</p>
    <p>Eksik besin alanları toplamların ve ortalamaların kısmi ya da bilinmiyor olarak gösterilmesine neden olabilir.</p>
    <p>Bu rapor tıbbi tavsiye veya tanı niteliği taşımaz.</p>
  </footer>
</body>
</html>`;
}
