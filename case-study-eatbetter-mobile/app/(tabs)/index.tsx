import { router, useFocusEffect } from 'expo-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  AppState,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';

import { calculateDailyTotals } from '../../src/domain/dailyTotals';
import { getMealsForLocalDay } from '../../src/domain/localDate';
import { DailySummaryCard } from '../../src/features/home/DailySummaryCard';
import { EmptyDayState } from '../../src/features/home/EmptyDayState';
import { MealRow } from '../../src/features/home/MealRow';
import { useMeals } from '../../src/state/MealStoreProvider';

export default function HomeScreen() {
  const { hydrationStatus, meals, retryHydration } = useMeals();
  const [referenceDate, setReferenceDate] = useState(() => new Date());

  const refreshReferenceDate = useCallback(() => {
    setReferenceDate(new Date());
  }, []);

  useFocusEffect(
    useCallback(() => {
      refreshReferenceDate();
    }, [refreshReferenceDate]),
  );

  useEffect(() => {
    const subscription = AppState.addEventListener('change', (nextState) => {
      if (nextState === 'active') {
        refreshReferenceDate();
      }
    });

    return () => subscription.remove();
  }, [refreshReferenceDate]);

  const formattedToday = useMemo(
    () =>
      new Intl.DateTimeFormat('tr-TR', {
        day: 'numeric',
        month: 'long',
        weekday: 'long',
        year: 'numeric',
      }).format(referenceDate),
    [referenceDate],
  );

  const readyDayData = useMemo(() => {
    if (hydrationStatus !== 'ready') {
      return null;
    }

    const todayMeals = getMealsForLocalDay(meals, referenceDate);

    return {
      meals: todayMeals,
      totals: calculateDailyTotals(todayMeals),
    };
  }, [hydrationStatus, meals, referenceDate]);

  return (
    <ScrollView contentContainerStyle={styles.container} style={styles.screen}>
      <View>
        <Text style={styles.eyebrow}>Bugün</Text>
        <Text style={styles.date}>{formattedToday}</Text>
      </View>

      <Pressable
        accessibilityRole="button"
        onPress={() => router.push('/search')}
        style={({ pressed }) => [styles.searchButton, pressed && styles.searchButtonPressed]}
      >
        <Text style={styles.searchButtonText}>Yiyecek Ara</Text>
      </Pressable>

      {hydrationStatus === 'hydrating' ? (
        <View style={styles.stateCard}>
          <View style={styles.inlineState}>
            <ActivityIndicator color="#28785f" />
            <Text style={styles.stateText}>Kayıtlı öğünler yükleniyor…</Text>
          </View>
        </View>
      ) : null}

      {hydrationStatus === 'error' ? (
        <View style={styles.stateCard}>
          <View style={styles.errorState}>
            <Text style={styles.errorTitle}>Öğünler yüklenemedi</Text>
            <Text style={styles.stateText}>
              Kayıtlı verilerinize şu anda erişilemiyor. Lütfen tekrar deneyin.
            </Text>
            <Pressable accessibilityRole="button" onPress={retryHydration} style={styles.retryButton}>
              <Text style={styles.retryButtonText}>Tekrar Dene</Text>
            </Pressable>
          </View>
        </View>
      ) : null}

      {readyDayData !== null ? (
        <>
          <DailySummaryCard totals={readyDayData.totals} />

          {readyDayData.meals.length === 0 ? (
            <EmptyDayState />
          ) : (
            <View style={styles.mealSection}>
              <Text style={styles.sectionTitle}>Bugünün öğünleri</Text>
              <View style={styles.meals}>
                {readyDayData.meals.map((meal, index) => (
                  <MealRow key={`${meal.id}-${index}`} meal={meal} />
                ))}
              </View>
            </View>
          )}
        </>
      ) : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#f7faf8' },
  container: { gap: 24, padding: 24, paddingBottom: 44 },
  eyebrow: { color: '#1d2b26', fontSize: 34, fontWeight: '700' },
  date: {
    marginTop: 6,
    color: '#64716c',
    fontSize: 17,
    textTransform: 'capitalize',
  },
  searchButton: {
    alignItems: 'center',
    borderRadius: 16,
    paddingHorizontal: 20,
    paddingVertical: 17,
    backgroundColor: '#28785f',
  },
  searchButtonPressed: { opacity: 0.82 },
  searchButtonText: { color: '#ffffff', fontSize: 17, fontWeight: '700' },
  stateCard: {
    minHeight: 92,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  inlineState: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  errorState: { gap: 10 },
  errorTitle: { color: '#7a3028', fontSize: 16, fontWeight: '700' },
  stateText: { color: '#52605b', fontSize: 15, lineHeight: 22 },
  retryButton: {
    alignSelf: 'flex-start',
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    backgroundColor: '#e6f2ed',
  },
  retryButtonText: { color: '#1f664f', fontWeight: '700' },
  mealSection: { gap: 13 },
  sectionTitle: { color: '#1d2b26', fontSize: 19, fontWeight: '700' },
  meals: { gap: 12 },
});
