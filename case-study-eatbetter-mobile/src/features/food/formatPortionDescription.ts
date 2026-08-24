import type { FoodPortion } from '../../domain/food';

const UNICODE_FRACTION_CHARACTERS = '¼½¾⅐⅑⅒⅓⅔⅕⅖⅗⅘⅙⅚⅛⅜⅝⅞';
const LEADING_EXPLICIT_AMOUNT_PATTERN = new RegExp(
  `^(?:\\d+\\s+\\d+\\s*\\/\\s*\\d+|\\d+\\s*\\/\\s*\\d+|\\d+\\s*[${UNICODE_FRACTION_CHARACTERS}]|[${UNICODE_FRACTION_CHARACTERS}]|\\d+(?:\\.\\d+)?)(?=\\s|$)`,
);

export function formatPortionAmountAndMeasure(
  amount: number,
  measure: string,
  formatAmount: (value: number) => string = String,
): string {
  const trimmedMeasure = measure.trim();

  return LEADING_EXPLICIT_AMOUNT_PATTERN.test(trimmedMeasure)
    ? trimmedMeasure
    : `${formatAmount(amount)} ${trimmedMeasure}`.trim();
}

export function formatPortionDescription(portion: FoodPortion): string {
  const amountAndMeasure = formatPortionAmountAndMeasure(portion.amount, portion.measure);

  return `${amountAndMeasure} · ${portion.grams} g`;
}
