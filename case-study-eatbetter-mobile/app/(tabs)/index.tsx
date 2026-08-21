import { router } from 'expo-router';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';

import { useMeals } from '../../src/state/MealStoreProvider';

const formattedToday = new Intl.DateTimeFormat('tr-TR', {
  day: 'numeric',
  month: 'long',
  weekday: 'long',
}).format(new Date());

export default function HomeScreen() {
  const { hydrationError, hydrationStatus, retryHydration } = useMeals();

  return (
    <View style={styles.container}>
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

      <View style={styles.stateCard}>
        {hydrationStatus === 'hydrating' ? (
          <View style={styles.inlineState}>
            <ActivityIndicator color="#28785f" />
            <Text style={styles.stateText}>Kayıtlı öğünler yükleniyor…</Text>
          </View>
        ) : null}

        {hydrationStatus === 'error' ? (
          <View style={styles.errorState}>
            <Text style={styles.errorTitle}>Öğünler yüklenemedi</Text>
            <Text style={styles.stateText}>
              Kayıtlı verilerinize şu anda erişilemiyor. Lütfen tekrar deneyin.
            </Text>
            <Pressable accessibilityRole="button" onPress={retryHydration} style={styles.retryButton}>
              <Text style={styles.retryButtonText}>Tekrar Dene</Text>
            </Pressable>
          </View>
        ) : null}

        {hydrationStatus === 'ready' && hydrationError === null ? (
          <Text style={styles.stateText}>Öğün takibi için hazırsınız.</Text>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    gap: 28,
    padding: 24,
    backgroundColor: '#f7faf8',
  },
  eyebrow: {
    color: '#1d2b26',
    fontSize: 34,
    fontWeight: '700',
  },
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
  searchButtonPressed: {
    opacity: 0.82,
  },
  searchButtonText: {
    color: '#ffffff',
    fontSize: 17,
    fontWeight: '700',
  },
  stateCard: {
    minHeight: 92,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  inlineState: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  errorState: {
    gap: 10,
  },
  errorTitle: {
    color: '#7a3028',
    fontSize: 16,
    fontWeight: '700',
  },
  stateText: {
    color: '#52605b',
    fontSize: 15,
    lineHeight: 22,
  },
  retryButton: {
    alignSelf: 'flex-start',
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    backgroundColor: '#e6f2ed',
  },
  retryButtonText: {
    color: '#1f664f',
    fontWeight: '700',
  },
});
