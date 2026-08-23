import type { NutritionValues } from './nutrition';

export type MealAiIntent = {
  query: string;
  quantity: number | null;
  unitHint: string | null;
};

export type MealAiResolvedFood = {
  foodId: number;
  displayName: string;
  canonicalName: string;
  brand: string | null;
};

export type MealAiFoodCandidate = {
  foodId: number;
  displayName: string;
  canonicalName: string;
  brand: string | null;
};

export type MealAiPortionOption = {
  portionId: number;
  amount: number;
  measure: string;
  grams: number;
};

export type MealAiGramsSelection = {
  kind: 'grams';
  foodId: number;
  grams: number;
};

export type MealAiStoredPortionSelection = {
  kind: 'portion';
  foodId: number;
  portion: {
    portionId: number;
    quantity: number;
    amount: number;
    measure: string;
    portionGrams: number;
  };
};

export type MealAiSelection = MealAiGramsSelection | MealAiStoredPortionSelection;

export type MealAiNutritionPreview = {
  resolvedGrams: number;
  nutrition: NutritionValues;
};

export type MealAiFoodIdentityClarification = {
  kind: 'food_identity';
  reason: string;
  candidates: MealAiFoodCandidate[];
  portions: [];
  allowDirectGrams: false;
};

export type MealAiAmountClarification = {
  kind: 'amount';
  reason: string;
  candidates: [];
  portions: MealAiPortionOption[];
  allowDirectGrams: true;
};

type MealAiInterpretedItemBase = {
  mention: string;
  intent: MealAiIntent;
};

export type ReadyMealAiItem = MealAiInterpretedItemBase & {
  state: 'ready';
  food: MealAiResolvedFood;
  selection: MealAiSelection;
  preview: MealAiNutritionPreview;
};

export type FoodIdentityClarificationMealAiItem = MealAiInterpretedItemBase & {
  state: 'clarification_required';
  food: null;
  clarification: MealAiFoodIdentityClarification;
};

export type AmountClarificationMealAiItem = MealAiInterpretedItemBase & {
  state: 'clarification_required';
  food: MealAiResolvedFood;
  clarification: MealAiAmountClarification;
};

export type ClarificationRequiredMealAiItem =
  | FoodIdentityClarificationMealAiItem
  | AmountClarificationMealAiItem;

export type MealAiItem = ReadyMealAiItem | ClarificationRequiredMealAiItem;

type ImageMealAiInterpretedItemBase = {
  observation: string;
  intent: MealAiIntent;
};

export type ReadyImageMealAiItem = ImageMealAiInterpretedItemBase & {
  state: 'ready';
  food: MealAiResolvedFood;
  selection: MealAiSelection;
  preview: MealAiNutritionPreview;
};

export type FoodIdentityClarificationImageMealAiItem = ImageMealAiInterpretedItemBase & {
  state: 'clarification_required';
  food: null;
  clarification: MealAiFoodIdentityClarification;
};

export type AmountClarificationImageMealAiItem = ImageMealAiInterpretedItemBase & {
  state: 'clarification_required';
  food: MealAiResolvedFood;
  clarification: MealAiAmountClarification;
};

export type ClarificationRequiredImageMealAiItem =
  | FoodIdentityClarificationImageMealAiItem
  | AmountClarificationImageMealAiItem;

export type ImageMealAiItem = ReadyImageMealAiItem | ClarificationRequiredImageMealAiItem;

export type EmptyMealInterpretResult = {
  state: 'empty';
  items: [];
};

export type ReadyMealInterpretResult = {
  state: 'ready';
  items: ReadyMealAiItem[];
};

export type ClarificationRequiredMealInterpretResult = {
  state: 'clarification_required';
  items: MealAiItem[];
};

export type MealInterpretResult =
  | EmptyMealInterpretResult
  | ReadyMealInterpretResult
  | ClarificationRequiredMealInterpretResult;

export type EmptyImageMealInterpretResult = {
  state: 'empty';
  items: [];
};

export type ReadyImageMealInterpretResult = {
  state: 'ready';
  items: ReadyImageMealAiItem[];
};

export type ClarificationRequiredImageMealInterpretResult = {
  state: 'clarification_required';
  items: ImageMealAiItem[];
};

export type ImageMealInterpretResult =
  | EmptyImageMealInterpretResult
  | ReadyImageMealInterpretResult
  | ClarificationRequiredImageMealInterpretResult;

export type ReadyMealAiResolveResult = {
  state: 'ready';
  intent: MealAiIntent;
  food: MealAiResolvedFood;
  selection: MealAiSelection;
  preview: MealAiNutritionPreview;
};

export type ClarificationRequiredMealAiResolveResult = {
  state: 'clarification_required';
  intent: MealAiIntent;
  food: MealAiResolvedFood;
  clarification: MealAiAmountClarification;
};

export type MealAiResolveResult =
  | ReadyMealAiResolveResult
  | ClarificationRequiredMealAiResolveResult;

export type MealAiResolveChoice =
  | { kind: 'food_identity' }
  | { kind: 'grams'; grams: number }
  | { kind: 'portion'; portionId: number; quantity: number };

export type ResolveMealSelectionInput = {
  foodId: number;
  locale: string;
  intent: MealAiIntent;
  choice: MealAiResolveChoice;
};
