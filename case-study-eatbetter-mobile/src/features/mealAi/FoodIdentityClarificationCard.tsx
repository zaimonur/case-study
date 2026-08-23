import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';

import type { FoodIdentityClarificationMealAiItem } from '../../domain/mealAi';
import type { MealAiResolveRuntime } from './mealAiSession';

type FoodIdentityClarificationCardProps = {
  item: FoodIdentityClarificationMealAiItem;
  itemIndex: number;
  onSelectCandidate: (itemIndex: number, foodId: number) => Promise<void>;
  resolve: MealAiResolveRuntime;
};

export function FoodIdentityClarificationCard({
  item,
  itemIndex,
  onSelectCandidate,
  resolve,
}: FoodIdentityClarificationCardProps) {
  const isResolving = resolve.status === 'resolving';
  const candidates = item.clarification.candidates;

  return (
    <View style={styles.card}>
      <Text style={styles.eyebrow}>Yiyecek seçimi</Text>
      <Text style={styles.question}>“{item.mention}” için hangisini kastettin?</Text>

      {candidates.length === 0 ? (
        <Text style={styles.guidance}>
          Bu yiyecek için güvenilir bir seçenek bulamadım. Açıklamayı düzenleyebilirsin.
        </Text>
      ) : (
        <View style={styles.candidates}>
          {candidates.map((candidate, candidateIndex) => (
            <Pressable
              accessibilityRole="button"
              accessibilityState={{ disabled: isResolving }}
              disabled={isResolving}
              key={`${candidate.foodId}-${candidateIndex}`}
              onPress={() => void onSelectCandidate(itemIndex, candidate.foodId)}
              style={({ pressed }) => [
                styles.candidate,
                isResolving && styles.candidateDisabled,
                pressed && !isResolving && styles.candidatePressed,
              ]}
            >
              <Text style={styles.candidateName}>{candidate.displayName}</Text>
              {candidate.canonicalName !== candidate.displayName ? (
                <Text style={styles.canonicalName}>{candidate.canonicalName}</Text>
              ) : null}
              {candidate.brand !== null ? (
                <Text style={styles.brand}>{candidate.brand}</Text>
              ) : null}
            </Pressable>
          ))}
        </View>
      )}

      {isResolving ? (
        <View accessibilityLiveRegion="polite" style={styles.resolveStatus}>
          <ActivityIndicator color="#28785f" size="small" />
          <Text style={styles.resolveStatusText}>Seçim doğrulanıyor…</Text>
        </View>
      ) : null}

      {resolve.status === 'error' ? (
        <Text accessibilityLiveRegion="polite" style={styles.errorText}>
          Seçim doğrulanamadı. Tekrar deneyebilirsin.
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    gap: 11,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 14,
    padding: 15,
    backgroundColor: '#f8faf9',
  },
  eyebrow: { color: '#28785f', fontSize: 13, fontWeight: '700' },
  question: { color: '#1d2b26', fontSize: 16, fontWeight: '700', lineHeight: 23 },
  guidance: { color: '#52605b', fontSize: 14, lineHeight: 21 },
  candidates: { gap: 9 },
  candidate: {
    minHeight: 58,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 12,
    paddingHorizontal: 14,
    paddingVertical: 12,
    backgroundColor: '#ffffff',
  },
  candidateDisabled: { opacity: 0.55 },
  candidatePressed: { borderColor: '#83b5a2', backgroundColor: '#eff7f3' },
  candidateName: { color: '#1d2b26', fontSize: 16, fontWeight: '700' },
  canonicalName: { marginTop: 4, color: '#64716c', fontSize: 13 },
  brand: { marginTop: 5, color: '#28785f', fontSize: 13, fontWeight: '600' },
  resolveStatus: { flexDirection: 'row', alignItems: 'center', gap: 9 },
  resolveStatusText: { color: '#52605b', fontSize: 14, lineHeight: 20 },
  errorText: { color: '#8e3b32', fontSize: 14, lineHeight: 20 },
});
