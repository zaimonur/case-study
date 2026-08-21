import { useFocusEffect } from 'expo-router';
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

import { analyzeLastSevenLocalDays } from '../../src/domain/analysis';
import {
  AnalysisOverviewCard,
  AverageNutritionCard,
  CalorieTrendCard,
  NutritionCoverageCard,
  TodayNutritionCard,
} from '../../src/features/analysis/AnalysisCards';
import { useMeals } from '../../src/state/MealStoreProvider';

export default function AnalysisScreen() {
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

  const analysis = useMemo(
    () =>
      hydrationStatus === 'ready'
        ? analyzeLastSevenLocalDays(meals, referenceDate)
        : null,
    [hydrationStatus, meals, referenceDate],
  );

  return (
    <ScrollView contentContainerStyle={styles.container} style={styles.screen}>
      <View>
        <Text style={styles.title}>Analiz</Text>
        <Text style={styles.subtitle}>Son 7 günlük kayıtlarının özeti</Text>
      </View>

      {hydrationStatus === 'hydrating' ? (
        <View style={styles.stateCard}>
          <View style={styles.loadingState}>
            <ActivityIndicator color="#28785f" />
            <Text style={styles.stateText}>Analiz verileri hazırlanıyor…</Text>
          </View>
        </View>
      ) : null}

      {hydrationStatus === 'error' ? (
        <View style={styles.stateCard}>
          <View style={styles.errorState}>
            <Text style={styles.errorTitle}>Kayıtlı öğünler yüklenemedi.</Text>
            <Text style={styles.stateText}>
              Analiz verilerine şu anda erişilemiyor. Lütfen tekrar deneyin.
            </Text>
            <Pressable
              accessibilityRole="button"
              onPress={retryHydration}
              style={({ pressed }) => [
                styles.retryButton,
                pressed && styles.retryButtonPressed,
              ]}
            >
              <Text style={styles.retryButtonText}>Tekrar Dene</Text>
            </Pressable>
          </View>
        </View>
      ) : null}

      {analysis !== null ? (
        <>
          <TodayNutritionCard day={analysis.days[0]} />
          <AnalysisOverviewCard analysis={analysis} />
          <AverageNutritionCard averageNutrition={analysis.averageNutrition} />
          <CalorieTrendCard days={analysis.days} />
          <NutritionCoverageCard coverage={analysis.nutritionCoverage} />
        </>
      ) : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#f7faf8' },
  container: { gap: 20, padding: 24, paddingBottom: 44 },
  title: { color: '#1d2b26', fontSize: 34, fontWeight: '700' },
  subtitle: { marginTop: 6, color: '#64716c', fontSize: 17, lineHeight: 24 },
  stateCard: {
    minHeight: 104,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  loadingState: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  errorState: { gap: 10 },
  errorTitle: { color: '#7a3028', fontSize: 16, fontWeight: '700' },
  stateText: { flexShrink: 1, color: '#52605b', fontSize: 15, lineHeight: 22 },
  retryButton: {
    alignSelf: 'flex-start',
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    backgroundColor: '#e6f2ed',
  },
  retryButtonPressed: { opacity: 0.76 },
  retryButtonText: { color: '#1f664f', fontWeight: '700' },
});
