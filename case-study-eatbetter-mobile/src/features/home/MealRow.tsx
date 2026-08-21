import { StyleSheet, Text, View } from 'react-native';

import type { MealRecord } from '../../domain/meal';

const timeFormatter = new Intl.DateTimeFormat('tr-TR', {
  hour: '2-digit',
  minute: '2-digit',
});

const numberFormatter = new Intl.NumberFormat('tr-TR', {
  maximumFractionDigits: 2,
});

function formatNumber(value: number): string {
  return numberFormatter.format(value);
}

function formatCalories(value: number | null): string {
  return value === null ? '—' : `${formatNumber(value)} kcal`;
}

export function MealRow({ meal }: { meal: MealRecord }) {
  const loggedDate = new Date(meal.loggedAt);
  const loggedTime = Number.isNaN(loggedDate.getTime()) ? '—' : timeFormatter.format(loggedDate);

  return (
    <View style={styles.card}>
      <Text style={styles.time}>{loggedTime}</Text>

      {meal.items.length === 0 ? (
        <Text style={styles.emptyItem}>Bu öğünde kayıtlı yiyecek bulunmuyor.</Text>
      ) : (
        <View style={styles.items}>
          {meal.items.map((item, index) => (
            <View key={`${item.foodId}-${index}`} style={styles.item}>
              <View style={styles.itemHeader}>
                <Text style={styles.name}>{item.displayName}</Text>
                <Text style={styles.calories}>{formatCalories(item.nutrition.caloriesKcal)}</Text>
              </View>
              {item.brand !== null ? <Text style={styles.brand}>{item.brand}</Text> : null}
              <Text style={styles.grams}>{formatNumber(item.resolvedGrams)} g</Text>
            </View>
          ))}
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 15,
    padding: 16,
    backgroundColor: '#ffffff',
  },
  time: { color: '#28785f', fontSize: 13, fontWeight: '700' },
  items: { gap: 14, marginTop: 10 },
  item: { gap: 5 },
  itemHeader: { flexDirection: 'row', alignItems: 'flex-start', gap: 14 },
  name: { flex: 1, color: '#1d2b26', fontSize: 16, fontWeight: '700' },
  calories: { color: '#35443e', fontSize: 14, fontWeight: '600' },
  brand: { color: '#64716c', fontSize: 13 },
  grams: { color: '#64716c', fontSize: 13 },
  emptyItem: { marginTop: 10, color: '#64716c', fontSize: 14 },
});
