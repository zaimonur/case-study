import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Keyboard,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import type { MealItem } from '../../src/domain/meal';
import type {
  ClarificationRequiredMealAiItem,
  FoodIdentityClarificationMealAiItem,
  ReadyMealAiItem,
} from '../../src/domain/mealAi';
import { createLocalMealRecord } from '../../src/domain/mealRecord';
import {
  AmountClarificationCard,
  type AmountResolveChoice,
} from '../../src/features/mealAi/AmountClarificationCard';
import { FoodIdentityClarificationCard } from '../../src/features/mealAi/FoodIdentityClarificationCard';
import {
  mapReadyMealAiItemToMealItem,
} from '../../src/features/mealAi/mapReadyMealAiItemToMealItem';
import { MealAiReviewCard } from '../../src/features/mealAi/MealAiReviewCard';
import { getMealAiErrorPresentation } from '../../src/features/mealAi/mealAiErrorPresentation';
import { isMealAiSessionFullyReady } from '../../src/features/mealAi/mealAiSession';
import type {
  MealAiSessionItem,
  MealAiSessionState,
} from '../../src/features/mealAi/mealAiSession';
import { useMealAiSession } from '../../src/features/mealAi/useMealAiSession';
import { useMeals } from '../../src/state/MealStoreProvider';

const MEAL_AI_LOCALE = 'tr-TR';

type SaveStatus = 'idle' | 'saving' | 'error' | 'success';

function isFoodIdentityClarificationItem(
  item: ClarificationRequiredMealAiItem,
): item is FoodIdentityClarificationMealAiItem {
  return item.clarification.kind === 'food_identity';
}

function ReadyMealAiItemCard({ item }: { item: ReadyMealAiItem }) {
  return (
    <View style={[styles.itemCard, styles.readyItemCard]}>
      <Text style={styles.readyLabel}>Hazır</Text>
      <Text style={styles.itemName}>{item.food.displayName}</Text>
      {item.food.brand !== null ? <Text style={styles.itemDetail}>{item.food.brand}</Text> : null}
    </View>
  );
}

function mapFullyReadySessionToMealItems(state: MealAiSessionState): MealItem[] | null {
  if (state.status !== 'active' || !isMealAiSessionFullyReady(state)) {
    return null;
  }

  const mealItems: MealItem[] = [];
  for (const sessionItem of state.items) {
    if (sessionItem.item.state !== 'ready') {
      return null;
    }

    mealItems.push(mapReadyMealAiItemToMealItem(sessionItem.item));
  }

  return mealItems;
}

export default function AiScreen() {
  const { state, interpret, reset, resolveItem } = useMealAiSession();
  const { addMeal, hydrationStatus } = useMeals();
  const [draft, setDraft] = useState('');
  const [saveStatus, setSaveStatus] = useState<SaveStatus>('idle');
  const lastSubmittedTextRef = useRef<string | null>(null);
  const completedSessionInvalidatedRef = useRef(false);
  const interpretCommandInFlightRef = useRef(false);
  const saveInFlightRef = useRef(false);
  const invalidatedReviewItemsRef = useRef(new WeakSet<MealAiSessionItem[]>());
  const mountedRef = useRef(true);

  const isInterpreting = state.status === 'interpreting';
  const saveIsRunning = saveStatus === 'saving';
  const canSubmit = state.status === 'idle' && draft.trim().length > 0 && !saveIsRunning;
  const retryText = lastSubmittedTextRef.current;
  const interpretErrorPresentation =
    state.status === 'error' ? getMealAiErrorPresentation(state.error) : null;
  const canRetry =
    state.status === 'error' &&
    interpretErrorPresentation?.retryable === true &&
    retryText !== null &&
    retryText.trim().length > 0 &&
    !saveIsRunning;
  const readyMealItems = useMemo(() => mapFullyReadySessionToMealItems(state), [state]);

  useEffect(() => {
    mountedRef.current = true;

    return () => {
      mountedRef.current = false;
    };
  }, []);

  const runInterpret = useCallback(
    async (submittedText: string) => {
      if (interpretCommandInFlightRef.current) {
        return;
      }

      interpretCommandInFlightRef.current = true;
      try {
        await interpret(submittedText, MEAL_AI_LOCALE);
      } finally {
        interpretCommandInFlightRef.current = false;
      }
    },
    [interpret],
  );

  const submitDraft = useCallback(async () => {
    if (!canSubmit || interpretCommandInFlightRef.current || saveInFlightRef.current) {
      return;
    }

    const submittedText = draft;
    lastSubmittedTextRef.current = submittedText;
    completedSessionInvalidatedRef.current = false;
    setSaveStatus('idle');
    Keyboard.dismiss();
    await runInterpret(submittedText);
  }, [canSubmit, draft, runInterpret]);

  const retryInterpretation = useCallback(async () => {
    const submittedText = lastSubmittedTextRef.current;
    if (
      state.status !== 'error' ||
      !getMealAiErrorPresentation(state.error).retryable ||
      submittedText === null ||
      submittedText.trim().length === 0 ||
      interpretCommandInFlightRef.current ||
      saveInFlightRef.current
    ) {
      return;
    }

    completedSessionInvalidatedRef.current = false;
    setSaveStatus('idle');
    Keyboard.dismiss();
    await runInterpret(submittedText);
  }, [runInterpret, state]);

  const updateDraft = useCallback(
    (nextDraft: string) => {
      if (interpretCommandInFlightRef.current || saveInFlightRef.current) {
        return;
      }

      if (nextDraft !== draft) {
        setSaveStatus('idle');
      }

      if (
        nextDraft !== draft &&
        (state.status === 'active' || state.status === 'empty' || state.status === 'error') &&
        !completedSessionInvalidatedRef.current
      ) {
        if (state.status === 'active') {
          invalidatedReviewItemsRef.current.add(state.items);
        }

        completedSessionInvalidatedRef.current = true;
        lastSubmittedTextRef.current = null;
        reset();
      }

      setDraft(nextDraft);
    },
    [draft, reset, state],
  );

  const startNewEntry = useCallback(() => {
    if (interpretCommandInFlightRef.current || saveInFlightRef.current) {
      return;
    }

    if (state.status === 'active') {
      invalidatedReviewItemsRef.current.add(state.items);
    }

    reset();
    setDraft('');
    setSaveStatus('idle');
    lastSubmittedTextRef.current = null;
    completedSessionInvalidatedRef.current = false;
    interpretCommandInFlightRef.current = false;
    Keyboard.dismiss();
  }, [reset, state]);

  const selectFoodCandidate = useCallback(
    async (itemIndex: number, foodId: number) => {
      try {
        await resolveItem(itemIndex, {
          kind: 'food_identity',
          foodId,
        });
      } catch {
        // Task 2 rejects stale, duplicate, or otherwise invalid local commands.
      }
    },
    [resolveItem],
  );

  const confirmAmount = useCallback(
    async (itemIndex: number, choice: AmountResolveChoice) => {
      try {
        if (choice.kind === 'grams') {
          await resolveItem(itemIndex, {
            kind: 'grams',
            grams: choice.grams,
          });
        } else {
          await resolveItem(itemIndex, {
            kind: 'portion',
            portionId: choice.portionId,
            quantity: choice.quantity,
          });
        }
      } catch {
        // Task 2 rejects stale, duplicate, or otherwise invalid local commands.
      }
    },
    [resolveItem],
  );

  const saveReadyMeal = useCallback(async () => {
    if (
      state.status !== 'active' ||
      !isMealAiSessionFullyReady(state) ||
      invalidatedReviewItemsRef.current.has(state.items) ||
      hydrationStatus !== 'ready' ||
      saveInFlightRef.current
    ) {
      return;
    }

    saveInFlightRef.current = true;
    setSaveStatus('saving');

    try {
      const currentMealItems = mapFullyReadySessionToMealItems(state);
      if (currentMealItems === null) {
        setSaveStatus('error');
        return;
      }

      const mealRecord = createLocalMealRecord(currentMealItems);
      await addMeal(mealRecord);

      if (!mountedRef.current) {
        return;
      }

      invalidatedReviewItemsRef.current.add(state.items);
      reset();
      setDraft('');
      lastSubmittedTextRef.current = null;
      completedSessionInvalidatedRef.current = false;
      interpretCommandInFlightRef.current = false;
      setSaveStatus('success');
      Keyboard.dismiss();
    } catch {
      if (mountedRef.current) {
        setSaveStatus('error');
      }
    } finally {
      saveInFlightRef.current = false;
    }
  }, [addMeal, hydrationStatus, reset, state]);

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      style={styles.screen}
    >
      <ScrollView
        contentContainerStyle={styles.container}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.header}>
          <Text style={styles.eyebrow}>Akıllı öğün girişi</Text>
          <Text style={styles.title}>AI ile Öğün Ekle</Text>
          <Text style={styles.description}>Ne yediğini doğal şekilde yaz.</Text>
        </View>

        <View style={styles.entryCard}>
          <Text style={styles.inputLabel}>Öğün açıklaması</Text>
          <TextInput
            accessibilityLabel="Öğün açıklaması"
            autoCapitalize="sentences"
            autoCorrect
            editable={!isInterpreting && !saveIsRunning}
            multiline
            onChangeText={updateDraft}
            placeholder="Örn. 2 yumurta ve 200 g tavuk yedim."
            placeholderTextColor="#87928e"
            style={[
              styles.input,
              (isInterpreting || saveIsRunning) && styles.inputDisabled,
            ]}
            textAlignVertical="top"
            value={draft}
          />

          <Pressable
            accessibilityRole="button"
            accessibilityState={{ disabled: !canSubmit }}
            disabled={!canSubmit}
            onPress={() => void submitDraft()}
            style={({ pressed }) => [
              styles.primaryButton,
              !canSubmit && styles.primaryButtonDisabled,
              pressed && canSubmit && styles.buttonPressed,
            ]}
          >
            <Text
              style={[
                styles.primaryButtonText,
                !canSubmit && styles.primaryButtonTextDisabled,
              ]}
            >
              Yorumla
            </Text>
          </Pressable>
        </View>

        {saveStatus === 'success' ? (
          <View accessibilityLiveRegion="polite" style={[styles.stateCard, styles.successCard]}>
            <Text style={styles.successTitle}>Öğün günlüğe eklendi.</Text>
          </View>
        ) : null}

        {state.status === 'interpreting' ? (
          <View accessibilityLiveRegion="polite" style={styles.stateCard}>
            <View style={styles.loadingRow}>
              <ActivityIndicator color="#28785f" />
              <View style={styles.loadingCopy}>
                <Text style={styles.stateTitle}>Öğünün yorumlanıyor…</Text>
                <Text style={styles.stateText}>Yiyecekleri ve miktarları anlamaya çalışıyorum.</Text>
              </View>
            </View>
          </View>
        ) : null}

        {state.status === 'empty' ? (
          <View accessibilityLiveRegion="polite" style={styles.stateCard}>
            <Text style={styles.stateTitle}>Yiyecek bulunamadı</Text>
            <Text style={styles.stateText}>
              Bu açıklamadan bir yiyecek çıkaramadım. Biraz daha ayrıntılı yazmayı deneyebilirsin.
            </Text>
            <Pressable
              accessibilityRole="button"
              onPress={startNewEntry}
              style={({ pressed }) => [styles.secondaryButton, pressed && styles.buttonPressed]}
            >
              <Text style={styles.secondaryButtonText}>Yeni giriş</Text>
            </Pressable>
          </View>
        ) : null}

        {state.status === 'error' ? (
          <View accessibilityLiveRegion="polite" style={[styles.stateCard, styles.errorCard]}>
            <Text style={styles.errorTitle}>Öğün yorumlanamadı</Text>
            <Text style={styles.stateText}>
              {interpretErrorPresentation?.message ?? 'Bu işlem şu anda tamamlanamıyor.'}
            </Text>
            <View style={styles.actionRow}>
              {canRetry ? (
                <Pressable
                  accessibilityRole="button"
                  onPress={() => void retryInterpretation()}
                  style={({ pressed }) => [
                    styles.retryButton,
                    pressed && styles.buttonPressed,
                  ]}
                >
                  <Text style={styles.retryButtonText}>Tekrar Dene</Text>
                </Pressable>
              ) : null}
              <Pressable
                accessibilityRole="button"
                onPress={startNewEntry}
                style={({ pressed }) => [styles.secondaryButton, pressed && styles.buttonPressed]}
              >
                <Text style={styles.secondaryButtonText}>Yeni giriş</Text>
              </Pressable>
            </View>
          </View>
        ) : null}

        {state.status === 'active' ? (
          <View accessibilityLiveRegion="polite" style={styles.stateCard}>
            {readyMealItems !== null ? (
              <MealAiReviewCard
                hydrationStatus={hydrationStatus}
                items={readyMealItems}
                onSave={saveReadyMeal}
                saveStatus={saveStatus === 'success' ? 'idle' : saveStatus}
              />
            ) : (
              <>
                <Text style={styles.stateTitle}>Öğünü netleştirelim</Text>
                <Text style={styles.stateText}>
                  Öğünü tamamlamak için birkaç ayrıntıyı netleştirmemiz gerekiyor.
                </Text>
                <View style={styles.sessionItems}>
                  {state.items.map((sessionItem, itemIndex) => {
                    const item = sessionItem.item;

                    if (item.state === 'ready') {
                      return <ReadyMealAiItemCard item={item} key={`meal-item-${itemIndex}`} />;
                    }

                    if (isFoodIdentityClarificationItem(item)) {
                      return (
                        <FoodIdentityClarificationCard
                          item={item}
                          itemIndex={itemIndex}
                          key={`meal-item-${itemIndex}`}
                          onSelectCandidate={selectFoodCandidate}
                          resolve={sessionItem.resolve}
                        />
                      );
                    }

                    return (
                      <AmountClarificationCard
                        item={item}
                        itemIndex={itemIndex}
                        key={`meal-item-${itemIndex}`}
                        onConfirm={confirmAmount}
                        resolve={sessionItem.resolve}
                      />
                    );
                  })}
                </View>
              </>
            )}
            <Pressable
              accessibilityRole="button"
              accessibilityState={{ disabled: saveIsRunning }}
              disabled={saveIsRunning}
              onPress={startNewEntry}
              style={({ pressed }) => [
                styles.secondaryButton,
                saveIsRunning && styles.secondaryButtonDisabled,
                pressed && !saveIsRunning && styles.buttonPressed,
              ]}
            >
              <Text
                style={[
                  styles.secondaryButtonText,
                  saveIsRunning && styles.secondaryButtonTextDisabled,
                ]}
              >
                Yeni giriş
              </Text>
            </Pressable>
          </View>
        ) : null}
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#f7faf8' },
  container: { gap: 22, padding: 24, paddingBottom: 48 },
  header: { gap: 7 },
  eyebrow: {
    color: '#28785f',
    fontSize: 14,
    fontWeight: '700',
    letterSpacing: 0.3,
  },
  title: { color: '#1d2b26', fontSize: 30, fontWeight: '700' },
  description: { color: '#64716c', fontSize: 16, lineHeight: 24 },
  entryCard: {
    gap: 14,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 18,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  inputLabel: { color: '#1d2b26', fontSize: 16, fontWeight: '700' },
  input: {
    minHeight: 132,
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 14,
    paddingHorizontal: 15,
    paddingVertical: 14,
    backgroundColor: '#ffffff',
    color: '#1d2b26',
    fontSize: 17,
    lineHeight: 24,
  },
  inputDisabled: { backgroundColor: '#f0f4f2', color: '#52605b' },
  primaryButton: {
    minHeight: 52,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 14,
    paddingHorizontal: 18,
    backgroundColor: '#28785f',
  },
  primaryButtonDisabled: { backgroundColor: '#dce5e1' },
  primaryButtonText: { color: '#ffffff', fontSize: 17, fontWeight: '700' },
  primaryButtonTextDisabled: { color: '#7c8984' },
  buttonPressed: { opacity: 0.78 },
  stateCard: {
    gap: 11,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  loadingRow: { flexDirection: 'row', alignItems: 'center', gap: 13 },
  loadingCopy: { flex: 1, gap: 3 },
  stateTitle: { color: '#1d2b26', fontSize: 17, fontWeight: '700' },
  stateText: { color: '#52605b', fontSize: 15, lineHeight: 22 },
  sessionItems: { gap: 12, marginTop: 2 },
  itemCard: {
    gap: 5,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 14,
    padding: 15,
    backgroundColor: '#f8faf9',
  },
  readyItemCard: { borderColor: '#b9d8ca', backgroundColor: '#eff7f3' },
  readyLabel: { color: '#28785f', fontSize: 13, fontWeight: '700' },
  itemName: { color: '#1d2b26', fontSize: 16, fontWeight: '700' },
  itemDetail: { color: '#64716c', fontSize: 14, lineHeight: 20 },
  errorCard: { borderColor: '#ead8d5' },
  errorTitle: { color: '#7a3028', fontSize: 17, fontWeight: '700' },
  successCard: { borderColor: '#b9d8ca', backgroundColor: '#eff7f3' },
  successTitle: { color: '#194c3c', fontSize: 16, fontWeight: '700' },
  actionRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 10, marginTop: 2 },
  retryButton: {
    minHeight: 44,
    justifyContent: 'center',
    borderRadius: 10,
    paddingHorizontal: 15,
    paddingVertical: 10,
    backgroundColor: '#e6f2ed',
  },
  retryButtonText: { color: '#1f664f', fontSize: 15, fontWeight: '700' },
  secondaryButton: {
    minHeight: 44,
    alignSelf: 'flex-start',
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 10,
    paddingHorizontal: 15,
    paddingVertical: 10,
    backgroundColor: '#ffffff',
  },
  secondaryButtonDisabled: { backgroundColor: '#eef2f0' },
  secondaryButtonText: { color: '#28785f', fontSize: 15, fontWeight: '700' },
  secondaryButtonTextDisabled: { color: '#87928e' },
});
