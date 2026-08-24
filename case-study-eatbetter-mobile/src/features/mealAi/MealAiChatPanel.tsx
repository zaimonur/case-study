import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Keyboard,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import type { MealItem, MealRecord } from '../../domain/meal';
import type {
  MealAiFoodCandidate,
  MealAiItem,
  ReadyMealAiItem,
} from '../../domain/mealAi';
import { createLocalMealRecord } from '../../domain/mealRecord';
import { useMeals } from '../../state/MealStoreProvider';
import { mapReadyMealAiItemToMealItem } from './mapReadyMealAiItemToMealItem';
import { MealAiReviewCard } from './MealAiReviewCard';
import { getMealAiErrorPresentation } from './mealAiErrorPresentation';
import { isMealAiChatSessionPristine } from './mealAiChatSession';
import { useMealAiChatSession } from './useMealAiChatSession';

const MEAL_AI_CHAT_LOCALE = 'tr-TR';
const MEAL_AI_CHAT_MAX_LENGTH = 2_000;

type SaveRuntime = 'idle' | 'saving' | 'error' | 'success';

type MealAiChatPanelProps = {
  onPristineChange: (isPristine: boolean) => void;
};

function formatNutritionValue(value: number | null, unit: string): string {
  return value === null ? '—' : `${value} ${unit}`;
}

function mapReadyItems(items: ReadyMealAiItem[]): MealItem[] {
  return items.map(mapReadyMealAiItemToMealItem);
}

function candidateBaseLabel(candidate: MealAiFoodCandidate): string {
  return candidate.brand === null
    ? candidate.displayName
    : `${candidate.displayName} — ${candidate.brand}`;
}

function candidateShortcutLabels(candidates: MealAiFoodCandidate[]): string[] {
  const baseLabels = candidates.map(candidateBaseLabel);
  return candidates.map((candidate, index) => {
    const baseLabel = baseLabels[index];
    const duplicateCount = baseLabels.filter((label) => label === baseLabel).length;
    if (duplicateCount <= 1 || candidate.canonicalName === candidate.displayName) {
      return baseLabel;
    }
    return `${baseLabel} (${candidate.canonicalName})`;
  });
}

function ChatMessageBubble({ role, text }: { role: 'user' | 'assistant'; text: string }) {
  const isUser = role === 'user';
  return (
    <View style={[styles.messageRow, isUser ? styles.userMessageRow : styles.assistantMessageRow]}>
      <View style={[styles.messageBubble, isUser ? styles.userBubble : styles.assistantBubble]}>
        <Text style={isUser ? styles.userMessageText : styles.assistantMessageText}>{text}</Text>
      </View>
    </View>
  );
}

function NutritionResult({ items }: { items: ReadyMealAiItem[] }) {
  return (
    <View style={styles.resultSection}>
      <View style={styles.resultHeader}>
        <Text style={styles.resultTitle}>Besin değerleri</Text>
        <Text style={styles.resultDescription}>Sunucunun doğruladığı yiyecek değerleri.</Text>
      </View>
      {items.map((item, itemIndex) => (
        <View key={`${item.food.foodId}-${itemIndex}`} style={styles.nutritionCard}>
          <Text style={styles.nutritionName}>{item.food.displayName}</Text>
          {item.food.brand !== null ? <Text style={styles.nutritionBrand}>{item.food.brand}</Text> : null}
          <View style={styles.nutritionRows}>
            <View style={styles.nutritionRow}>
              <Text style={styles.nutritionLabel}>Miktar</Text>
              <Text style={styles.nutritionValue}>{item.preview.resolvedGrams} g</Text>
            </View>
            <View style={styles.nutritionRow}>
              <Text style={styles.nutritionLabel}>Kalori</Text>
              <Text style={styles.nutritionValue}>
                {formatNutritionValue(item.preview.nutrition.caloriesKcal, 'kcal')}
              </Text>
            </View>
            <View style={styles.nutritionRow}>
              <Text style={styles.nutritionLabel}>Protein</Text>
              <Text style={styles.nutritionValue}>
                {formatNutritionValue(item.preview.nutrition.proteinG, 'g')}
              </Text>
            </View>
            <View style={styles.nutritionRow}>
              <Text style={styles.nutritionLabel}>Karbonhidrat</Text>
              <Text style={styles.nutritionValue}>
                {formatNutritionValue(item.preview.nutrition.carbohydratesG, 'g')}
              </Text>
            </View>
            <View style={styles.nutritionRow}>
              <Text style={styles.nutritionLabel}>Yağ</Text>
              <Text style={styles.nutritionValue}>
                {formatNutritionValue(item.preview.nutrition.fatG, 'g')}
              </Text>
            </View>
          </View>
        </View>
      ))}
    </View>
  );
}

function ClarificationItems({ items, activeItemIndex }: { items: MealAiItem[]; activeItemIndex: number }) {
  return (
    <View style={styles.clarificationItems}>
      {items.map((item, itemIndex) => (
        <View
          key={`chat-item-${itemIndex}`}
          style={[
            styles.clarificationItem,
            itemIndex === activeItemIndex && styles.activeClarificationItem,
          ]}
        >
          <Text style={styles.clarificationItemStatus}>
            {item.state === 'ready'
              ? 'Hazır'
              : itemIndex === activeItemIndex
                ? 'Şimdi netleştiriliyor'
                : 'Daha sonra netleştirilecek'}
          </Text>
          <Text style={styles.clarificationItemName}>
            {item.state === 'ready' || item.food !== null ? item.food.displayName : item.mention}
          </Text>
        </View>
      ))}
    </View>
  );
}

export function MealAiChatPanel({ onPristineChange }: MealAiChatPanelProps) {
  const { state, sendMessage, retryFailedTurn, reset } = useMealAiChatSession();
  const { addMeal, hydrationStatus } = useMeals();
  const [draft, setDraft] = useState('');
  const [saveRuntime, setSaveRuntime] = useState<SaveRuntime>('idle');
  const preparedMealRecordRef = useRef<MealRecord | null>(null);
  const saveInFlightRef = useRef(false);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const committedResult = state.committedResult;
  const turnIsPending = state.turnRuntime.status === 'pending';
  const turnIsFailed = state.turnRuntime.status === 'failed';
  const conversationIsTerminal =
    committedResult?.state === 'ready' || committedResult?.state === 'empty';
  const conversationIsPristine = isMealAiChatSessionPristine(state) && saveRuntime === 'idle';
  const composerAvailable =
    !turnIsPending &&
    !turnIsFailed &&
    !conversationIsTerminal &&
    saveRuntime !== 'saving';
  const canSubmit = composerAvailable && draft.trim().length > 0;
  const errorPresentation =
    state.turnRuntime.status === 'failed'
      ? getMealAiErrorPresentation(state.turnRuntime.error)
      : null;

  const activeClarificationItem = useMemo(() => {
    if (committedResult?.state !== 'clarification_required') {
      return null;
    }
    return committedResult.items[committedResult.activeItemIndex] ?? null;
  }, [committedResult]);
  const candidateLabels = useMemo(() => {
    if (
      activeClarificationItem?.state !== 'clarification_required' ||
      activeClarificationItem.clarification.kind !== 'food_identity'
    ) {
      return [];
    }
    return candidateShortcutLabels(activeClarificationItem.clarification.candidates);
  }, [activeClarificationItem]);

  const readyMealItems = useMemo(() => {
    if (
      committedResult?.state !== 'ready' ||
      committedResult.purpose !== 'meal_logging'
    ) {
      return null;
    }
    return mapReadyItems(committedResult.items);
  }, [committedResult]);

  const submitText = useCallback(
    async (visibleMessage: string): Promise<void> => {
      const submittedMessage = visibleMessage.trim();
      if (!composerAvailable || submittedMessage.length === 0) {
        return;
      }
      onPristineChange(false);
      setDraft('');
      Keyboard.dismiss();
      try {
        await sendMessage(submittedMessage, MEAL_AI_CHAT_LOCALE);
      } catch {
        // The controller rejects duplicate, stale, or terminal local commands.
      }
    },
    [composerAvailable, onPristineChange, sendMessage],
  );

  const retryTurn = useCallback(async (): Promise<void> => {
    if (errorPresentation?.retryable !== true) {
      return;
    }
    Keyboard.dismiss();
    try {
      await retryFailedTurn();
    } catch {
      // The controller owns retry eligibility and rejects stale commands.
    }
  }, [errorPresentation?.retryable, retryFailedTurn]);

  const saveReadyMeal = useCallback(async (): Promise<void> => {
    if (
      readyMealItems === null ||
      hydrationStatus !== 'ready' ||
      saveInFlightRef.current ||
      saveRuntime === 'success'
    ) {
      return;
    }

    const mealRecord =
      preparedMealRecordRef.current ?? createLocalMealRecord(readyMealItems);
    preparedMealRecordRef.current = mealRecord;
    saveInFlightRef.current = true;
    setSaveRuntime('saving');
    try {
      await addMeal(mealRecord);
      if (mountedRef.current) {
        setSaveRuntime('success');
      }
    } catch {
      if (mountedRef.current) {
        setSaveRuntime('error');
      }
    } finally {
      saveInFlightRef.current = false;
    }
  }, [addMeal, hydrationStatus, readyMealItems, saveRuntime]);

  const startNewChat = useCallback((): void => {
    if (saveInFlightRef.current) {
      return;
    }
    reset();
    setDraft('');
    setSaveRuntime('idle');
    preparedMealRecordRef.current = null;
    onPristineChange(true);
    Keyboard.dismiss();
  }, [onPristineChange, reset]);

  return (
    <View style={styles.panel}>
      {state.messages.length === 0 ? (
        <View style={styles.helperCard}>
          <Text style={styles.helperTitle}>Öğün asistanı</Text>
          <Text style={styles.helperText}>
            Ne yediğini yazabilir veya bir yiyeceğin besin değerini sorabilirsin.
          </Text>
        </View>
      ) : (
        <View style={styles.messages}>
          {state.messages.map((message) => (
            <ChatMessageBubble key={message.id} role={message.role} text={message.text} />
          ))}
        </View>
      )}

      {turnIsPending ? (
        <View accessibilityLiveRegion="polite" style={styles.typingRow}>
          <ActivityIndicator color="#28785f" size="small" />
          <Text style={styles.typingText}>Yanıt hazırlanıyor…</Text>
        </View>
      ) : null}

      {errorPresentation !== null ? (
        <View accessibilityLiveRegion="polite" style={styles.errorCard}>
          <Text style={styles.errorTitle}>Mesaj gönderilemedi</Text>
          <Text style={styles.errorText}>{errorPresentation.message}</Text>
          {errorPresentation.retryable ? (
            <Pressable
              accessibilityRole="button"
              onPress={() => void retryTurn()}
              style={({ pressed }) => [styles.retryButton, pressed && styles.buttonPressed]}
            >
              <Text style={styles.retryButtonText}>Tekrar Dene</Text>
            </Pressable>
          ) : null}
        </View>
      ) : null}

      {committedResult?.state === 'clarification_required' ? (
        <View style={styles.clarificationSection}>
          <ClarificationItems
            activeItemIndex={committedResult.activeItemIndex}
            items={committedResult.items}
          />
          {activeClarificationItem?.state === 'clarification_required' &&
          activeClarificationItem.clarification.kind === 'food_identity' &&
          candidateLabels.length > 0 ? (
            <View style={styles.shortcutSection}>
              <Text style={styles.shortcutLabel}>Hızlı seçim</Text>
              <View style={styles.shortcutList}>
                {candidateLabels.map((label, index) => (
                  <Pressable
                    accessibilityRole="button"
                    accessibilityState={{ disabled: !composerAvailable }}
                    disabled={!composerAvailable}
                    key={`${label}-${index}`}
                    onPress={() => void submitText(label)}
                    style={({ pressed }) => [
                      styles.shortcutChip,
                      !composerAvailable && styles.shortcutChipDisabled,
                      pressed && composerAvailable && styles.buttonPressed,
                    ]}
                  >
                    <Text style={styles.shortcutChipText}>{label}</Text>
                  </Pressable>
                ))}
              </View>
            </View>
          ) : null}
        </View>
      ) : null}

      {committedResult?.state === 'ready' && committedResult.purpose === 'meal_logging' && readyMealItems !== null ? (
        <MealAiReviewCard
          hydrationStatus={hydrationStatus}
          items={readyMealItems}
          onSave={saveReadyMeal}
          saveStatus={saveRuntime}
        />
      ) : null}

      {committedResult?.state === 'ready' && committedResult.purpose === 'nutrition_query' ? (
        <NutritionResult items={committedResult.items} />
      ) : null}

      {!conversationIsTerminal || turnIsFailed ? (
        <View style={styles.composerCard}>
          <TextInput
            accessibilityLabel="Öğün asistanına mesaj"
            autoCapitalize="sentences"
            autoCorrect
            editable={composerAvailable}
            maxLength={MEAL_AI_CHAT_MAX_LENGTH}
            multiline
            onChangeText={setDraft}
            placeholder={
              committedResult?.state === 'clarification_required'
                ? 'Yanıtını yaz…'
                : 'Örn. 150 g tavuk göğsü yedim'
            }
            placeholderTextColor="#87928e"
            style={[styles.composerInput, !composerAvailable && styles.composerInputDisabled]}
            textAlignVertical="top"
            value={draft}
          />
          <Pressable
            accessibilityRole="button"
            accessibilityState={{ disabled: !canSubmit }}
            disabled={!canSubmit}
            onPress={() => void submitText(draft)}
            style={({ pressed }) => [
              styles.sendButton,
              !canSubmit && styles.sendButtonDisabled,
              pressed && canSubmit && styles.buttonPressed,
            ]}
          >
            <Text style={[styles.sendButtonText, !canSubmit && styles.sendButtonTextDisabled]}>
              Gönder
            </Text>
          </Pressable>
        </View>
      ) : null}

      {!conversationIsPristine ? (
        <Pressable
          accessibilityRole="button"
          accessibilityState={{ disabled: saveRuntime === 'saving' }}
          disabled={saveRuntime === 'saving'}
          onPress={startNewChat}
          style={({ pressed }) => [
            styles.newChatButton,
            saveRuntime === 'saving' && styles.newChatButtonDisabled,
            pressed && saveRuntime !== 'saving' && styles.buttonPressed,
          ]}
        >
          <Text style={styles.newChatButtonText}>Yeni sohbet</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  panel: { gap: 16 },
  helperCard: {
    gap: 6,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  helperTitle: { color: '#1d2b26', fontSize: 17, fontWeight: '700' },
  helperText: { color: '#52605b', fontSize: 15, lineHeight: 22 },
  messages: { gap: 11 },
  messageRow: { flexDirection: 'row' },
  userMessageRow: { justifyContent: 'flex-end' },
  assistantMessageRow: { justifyContent: 'flex-start' },
  messageBubble: { maxWidth: '86%', borderRadius: 16, paddingHorizontal: 15, paddingVertical: 11 },
  userBubble: { borderBottomRightRadius: 5, backgroundColor: '#28785f' },
  assistantBubble: {
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderBottomLeftRadius: 5,
    backgroundColor: '#ffffff',
  },
  userMessageText: { color: '#ffffff', fontSize: 16, lineHeight: 22 },
  assistantMessageText: { color: '#1d2b26', fontSize: 16, lineHeight: 23 },
  typingRow: {
    alignSelf: 'flex-start',
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    borderRadius: 14,
    paddingHorizontal: 14,
    paddingVertical: 10,
    backgroundColor: '#eaf3ef',
  },
  typingText: { color: '#52605b', fontSize: 14 },
  errorCard: {
    gap: 8,
    borderWidth: 1,
    borderColor: '#ead8d5',
    borderRadius: 14,
    padding: 15,
    backgroundColor: '#fffafa',
  },
  errorTitle: { color: '#7a3028', fontSize: 16, fontWeight: '700' },
  errorText: { color: '#52605b', fontSize: 14, lineHeight: 21 },
  retryButton: {
    alignSelf: 'flex-start',
    minHeight: 42,
    justifyContent: 'center',
    borderRadius: 10,
    paddingHorizontal: 14,
    backgroundColor: '#e6f2ed',
  },
  retryButtonText: { color: '#1f664f', fontSize: 14, fontWeight: '700' },
  clarificationSection: { gap: 12 },
  clarificationItems: { gap: 9 },
  clarificationItem: {
    gap: 4,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 12,
    padding: 13,
    backgroundColor: '#f8faf9',
  },
  activeClarificationItem: { borderColor: '#83b5a2', backgroundColor: '#eff7f3' },
  clarificationItemStatus: { color: '#28785f', fontSize: 12, fontWeight: '700' },
  clarificationItemName: { color: '#1d2b26', fontSize: 15, fontWeight: '700' },
  shortcutSection: { gap: 8 },
  shortcutLabel: { color: '#52605b', fontSize: 13, fontWeight: '700' },
  shortcutList: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  shortcutChip: {
    minHeight: 40,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#a8c9bb',
    borderRadius: 20,
    paddingHorizontal: 13,
    backgroundColor: '#f7fbf9',
  },
  shortcutChipDisabled: { opacity: 0.48 },
  shortcutChipText: { color: '#1f664f', fontSize: 14, fontWeight: '700' },
  resultSection: { gap: 12 },
  resultHeader: { gap: 4 },
  resultTitle: { color: '#1d2b26', fontSize: 19, fontWeight: '700' },
  resultDescription: { color: '#52605b', fontSize: 14, lineHeight: 21 },
  nutritionCard: {
    borderWidth: 1,
    borderColor: '#b9d8ca',
    borderRadius: 14,
    padding: 15,
    backgroundColor: '#eff7f3',
  },
  nutritionName: { color: '#194c3c', fontSize: 17, fontWeight: '700' },
  nutritionBrand: { marginTop: 4, color: '#28785f', fontSize: 14, fontWeight: '600' },
  nutritionRows: { gap: 8, marginTop: 13 },
  nutritionRow: { flexDirection: 'row', justifyContent: 'space-between', gap: 16 },
  nutritionLabel: { flex: 1, color: '#52605b', fontSize: 14 },
  nutritionValue: { color: '#1d2b26', fontSize: 14, fontWeight: '700', textAlign: 'right' },
  composerCard: {
    gap: 10,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 13,
    backgroundColor: '#ffffff',
  },
  composerInput: {
    minHeight: 72,
    maxHeight: 150,
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 12,
    paddingHorizontal: 13,
    paddingVertical: 11,
    color: '#1d2b26',
    fontSize: 16,
    lineHeight: 22,
  },
  composerInputDisabled: { backgroundColor: '#f0f4f2', color: '#52605b' },
  sendButton: {
    minHeight: 46,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 12,
    backgroundColor: '#28785f',
  },
  sendButtonDisabled: { backgroundColor: '#dce5e1' },
  sendButtonText: { color: '#ffffff', fontSize: 15, fontWeight: '700' },
  sendButtonTextDisabled: { color: '#7c8984' },
  newChatButton: {
    minHeight: 44,
    alignSelf: 'flex-start',
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 10,
    paddingHorizontal: 15,
    backgroundColor: '#ffffff',
  },
  newChatButtonDisabled: { opacity: 0.5 },
  newChatButtonText: { color: '#28785f', fontSize: 15, fontWeight: '700' },
  buttonPressed: { opacity: 0.78 },
});
