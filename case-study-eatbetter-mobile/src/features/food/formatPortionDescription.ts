import type { FoodPortion } from '../../domain/food';

const AMOUNT_MATCH_TOLERANCE = 1e-9;
const LEADING_AMOUNT_PATTERN = /^(\d+\s*\/\s*\d+|\d+(?:\.\d+)?)(?=\s|$)/;

function parseLeadingAmount(value: string): number | null {
  if (value.includes('/')) {
    const [numeratorText, denominatorText] = value.split('/');
    const numerator = Number(numeratorText.trim());
    const denominator = Number(denominatorText.trim());

    if (!Number.isFinite(numerator) || !Number.isFinite(denominator) || denominator === 0) {
      return null;
    }

    return numerator / denominator;
  }

  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

function measureIncludesEquivalentAmount(amount: number, measure: string): boolean {
  const leadingAmountText = measure.match(LEADING_AMOUNT_PATTERN)?.[1];

  if (leadingAmountText === undefined) {
    return false;
  }

  const leadingAmount = parseLeadingAmount(leadingAmountText);
  return (
    leadingAmount !== null &&
    Math.abs(leadingAmount - amount) <= AMOUNT_MATCH_TOLERANCE
  );
}

export function formatPortionDescription(portion: FoodPortion): string {
  const measure = portion.measure.trim();
  const amountAndMeasure = measureIncludesEquivalentAmount(portion.amount, measure)
    ? measure
    : `${portion.amount} ${measure}`.trim();

  return `${amountAndMeasure} · ${portion.grams} g`;
}
