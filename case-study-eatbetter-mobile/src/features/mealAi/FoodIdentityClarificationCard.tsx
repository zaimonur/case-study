import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';

import type {
  FoodIdentityClarificationImageMealAiItem,
  FoodIdentityClarificationMealAiItem,
} from '../../domain/mealAi';
import { getMealAiErrorPresentation } from './mealAiErrorPresentation';
import type { MealAiResolveRuntime } from './mealAiSession';

type FoodIdentityClarificationCardProps = {
  item: FoodIdentityClarificationMealAiItem | FoodIdentityClarificationImageMealAiItem;
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
  const errorPresentation =
    resolve.status === 'error' ? getMealAiErrorPresentation(resolve.error) : null;
  const canResolve = !isResolving && (errorPresentation === null || errorPresentation.retryable);
  const candidates = item.clarification.candidates;
  const question =
    'mention' in item
      ? `“${item.mention}” için hangisini kastettin?`
      : `Fotoğraftaki “${item.observation}” için hangisi doğru?`;
  const guidance =
    'mention' in item
      ? 'Bu yiyecek için güvenilir bir seçenek bulamadım. Açıklamayı düzenleyebilirsin.'
      : 'Bu yiyecek için güvenilir bir seçenek bulamadım. Yeni giriş yapıp başka bir fotoğraf deneyebilirsin.';

  return (
    <View style={styles.card}>
      <Text style={styles.eyebrow}>Yiyecek seçimi</Text>
      <Text style={styles.question}>{question}</Text>

      {candidates.length === 0 ? (
        <Text style={styles.guidance}>{guidance}</Text>
      ) : (
        <View style={styles.candidates}>
          {candidates.map((candidate, candidateIndex) => (
            <Pressable
              accessibilityRole="button"
              accessibilityState={{ disabled: !canResolve }}
              disabled={!canResolve}
              key={`${candidate.foodId}-${candidateIndex}`}
              onPress={() => void onSelectCandidate(itemIndex, candidate.foodId)}
              style={({ pressed }) => [
                styles.candidate,
                !canResolve && styles.candidateDisabled,
                pressed && canResolve && styles.candidatePressed,
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

      {errorPresentation !== null ? (
        <Text accessibilityLiveRegion="polite" style={styles.errorText}>
          {errorPresentation.message}
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
