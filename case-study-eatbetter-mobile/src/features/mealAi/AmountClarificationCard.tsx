import { useMemo, useState } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';

import type { AmountClarificationMealAiItem } from '../../domain/mealAi';
import {
  PortionSelector,
  type PortionSelection,
} from '../food/PortionSelector';
import type { MealAiResolveRuntime, MealAiSessionResolveChoice } from './mealAiSession';

export type AmountResolveChoice = Exclude<
  MealAiSessionResolveChoice,
  { kind: 'food_identity' }
>;

type AmountClarificationCardProps = {
  item: AmountClarificationMealAiItem;
  itemIndex: number;
  onConfirm: (itemIndex: number, choice: AmountResolveChoice) => Promise<void>;
  resolve: MealAiResolveRuntime;
};

function parsePositiveNumberInput(value: string): number | null {
  const trimmedValue = value.trim();
  if (trimmedValue.length === 0) {
    return null;
  }

  const parsedValue = Number(trimmedValue);
  return Number.isFinite(parsedValue) && parsedValue > 0 ? parsedValue : null;
}

function buildAmountResolveChoice(
  item: AmountClarificationMealAiItem,
  selection: PortionSelection,
): AmountResolveChoice | null {
  if (selection.kind === 'grams') {
    const grams = parsePositiveNumberInput(selection.grams);
    return grams === null ? null : { kind: 'grams', grams };
  }

  if (selection.kind === 'portion') {
    const portion = item.clarification.portions.find(
      ({ portionId }) => portionId === selection.portionId,
    );
    const quantity = parsePositiveNumberInput(selection.quantity);

    if (portion === undefined || quantity === null) {
      return null;
    }

    return {
      kind: 'portion',
      portionId: portion.portionId,
      quantity,
    };
  }

  return null;
}

export function AmountClarificationCard({
  item,
  itemIndex,
  onConfirm,
  resolve,
}: AmountClarificationCardProps) {
  const [selection, setSelection] = useState<PortionSelection>({ kind: 'none' });
  const choice = useMemo(() => buildAmountResolveChoice(item, selection), [item, selection]);
  const isResolving = resolve.status === 'resolving';
  const canConfirm = choice !== null && !isResolving;

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.eyebrow}>Miktar seçimi</Text>
        <Text style={styles.foodName}>{item.food.displayName}</Text>
        <Text style={styles.guidance}>Ne kadar yediğini açıkça belirt.</Text>
      </View>

      <PortionSelector
        disabled={isResolving}
        isSelectionValid={choice !== null}
        onSelectionChange={setSelection}
        portions={item.clarification.portions}
        selection={selection}
      />

      <Pressable
        accessibilityRole="button"
        accessibilityState={{ disabled: !canConfirm }}
        disabled={!canConfirm}
        onPress={() => {
          if (choice !== null) {
            void onConfirm(itemIndex, choice);
          }
        }}
        style={({ pressed }) => [
          styles.confirmButton,
          !canConfirm && styles.confirmButtonDisabled,
          pressed && canConfirm && styles.confirmButtonPressed,
        ]}
      >
        <Text style={[styles.confirmButtonText, !canConfirm && styles.confirmButtonTextDisabled]}>
          Miktarı doğrula
        </Text>
      </Pressable>

      {isResolving ? (
        <View accessibilityLiveRegion="polite" style={styles.resolveStatus}>
          <ActivityIndicator color="#28785f" size="small" />
          <Text style={styles.resolveStatusText}>Miktar doğrulanıyor…</Text>
        </View>
      ) : null}

      {resolve.status === 'error' ? (
        <Text accessibilityLiveRegion="polite" style={styles.errorText}>
          Miktar doğrulanamadı. Tekrar deneyebilirsin.
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  card: { gap: 12 },
  header: {
    gap: 5,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 14,
    padding: 15,
    backgroundColor: '#f8faf9',
  },
  eyebrow: { color: '#28785f', fontSize: 13, fontWeight: '700' },
  foodName: { color: '#1d2b26', fontSize: 16, fontWeight: '700' },
  guidance: { color: '#64716c', fontSize: 14, lineHeight: 20 },
  confirmButton: {
    minHeight: 48,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 12,
    paddingHorizontal: 16,
    backgroundColor: '#28785f',
  },
  confirmButtonDisabled: { backgroundColor: '#dce5e1' },
  confirmButtonPressed: { opacity: 0.78 },
  confirmButtonText: { color: '#ffffff', fontSize: 15, fontWeight: '700' },
  confirmButtonTextDisabled: { color: '#7c8984' },
  resolveStatus: { flexDirection: 'row', alignItems: 'center', gap: 9 },
  resolveStatusText: { color: '#52605b', fontSize: 14, lineHeight: 20 },
  errorText: { color: '#8e3b32', fontSize: 14, lineHeight: 20 },
});
