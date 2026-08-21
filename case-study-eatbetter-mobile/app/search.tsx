import { router } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { isAbortError } from '../src/api/client';
import { searchFoods } from '../src/api/foods';
import type { FoodSearchItem } from '../src/domain/food';

type SearchStatus = 'guidance' | 'debouncing' | 'loading' | 'success' | 'empty' | 'error';

type SearchState = {
  status: SearchStatus;
  query: string;
  items: FoodSearchItem[];
};

const SEARCH_DEBOUNCE_MS = 300;

function FoodSearchResultRow({ item }: { item: FoodSearchItem }) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={() => router.push(`/food/${item.foodId}`)}
      style={({ pressed }) => [styles.resultRow, pressed && styles.resultRowPressed]}
    >
      <Text style={styles.resultName}>{item.displayName}</Text>
      {item.canonicalName !== item.displayName ? (
        <Text style={styles.canonicalName}>{item.canonicalName}</Text>
      ) : null}
      {item.brand !== null ? <Text style={styles.brand}>{item.brand}</Text> : null}
    </Pressable>
  );
}

export default function SearchScreen() {
  const [input, setInput] = useState('');
  const [searchState, setSearchState] = useState<SearchState>({
    status: 'guidance',
    query: '',
    items: [],
  });
  const activeQuery = input.trim();
  const activeQueryRef = useRef(activeQuery);
  const requestGenerationRef = useRef(0);
  const abortControllerRef = useRef<AbortController | null>(null);

  activeQueryRef.current = activeQuery;

  const runSearch = useCallback(async (query: string) => {
    abortControllerRef.current?.abort();

    const requestGeneration = requestGenerationRef.current + 1;
    requestGenerationRef.current = requestGeneration;
    const controller = new AbortController();
    abortControllerRef.current = controller;

    setSearchState({ status: 'loading', query, items: [] });

    try {
      const items = await searchFoods(query, controller.signal);

      if (
        controller.signal.aborted ||
        requestGeneration !== requestGenerationRef.current ||
        activeQueryRef.current !== query
      ) {
        return;
      }

      setSearchState({
        status: items.length === 0 ? 'empty' : 'success',
        query,
        items,
      });
    } catch (error) {
      if (
        isAbortError(error) ||
        controller.signal.aborted ||
        requestGeneration !== requestGenerationRef.current ||
        activeQueryRef.current !== query
      ) {
        return;
      }

      setSearchState({ status: 'error', query, items: [] });
    } finally {
      if (abortControllerRef.current === controller) {
        abortControllerRef.current = null;
      }
    }
  }, []);

  useEffect(() => {
    requestGenerationRef.current += 1;
    abortControllerRef.current?.abort();
    abortControllerRef.current = null;

    if (activeQuery.length < 2) {
      setSearchState({ status: 'guidance', query: activeQuery, items: [] });
      return;
    }

    const debounceGeneration = requestGenerationRef.current;
    setSearchState({ status: 'debouncing', query: activeQuery, items: [] });

    const timeout = setTimeout(() => {
      if (requestGenerationRef.current === debounceGeneration) {
        void runSearch(activeQuery);
      }
    }, SEARCH_DEBOUNCE_MS);

    return () => {
      clearTimeout(timeout);
      requestGenerationRef.current += 1;
      abortControllerRef.current?.abort();
      abortControllerRef.current = null;
    };
  }, [activeQuery, runSearch]);

  const displayedState: SearchState =
    searchState.query === activeQuery
      ? searchState
      : {
          status: activeQuery.length < 2 ? 'guidance' : 'debouncing',
          query: activeQuery,
          items: [],
        };

  const retry = () => {
    if (activeQuery.length >= 2) {
      void runSearch(activeQuery);
    }
  };

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Yiyecek Ara</Text>
      <TextInput
        autoCapitalize="none"
        autoCorrect={false}
        autoFocus
        onChangeText={setInput}
        placeholder="Örn. süt"
        returnKeyType="search"
        style={styles.input}
        value={input}
      />

      {displayedState.status === 'guidance' ? (
        <Text style={styles.feedbackText}>Aramak için en az 2 karakter yaz.</Text>
      ) : null}

      {displayedState.status === 'debouncing' ? (
        <Text style={styles.feedbackText}>Arama hazırlanıyor…</Text>
      ) : null}

      {displayedState.status === 'loading' ? (
        <View style={styles.loadingState}>
          <ActivityIndicator color="#28785f" />
          <Text style={styles.feedbackText}>Yiyecekler aranıyor…</Text>
        </View>
      ) : null}

      {displayedState.status === 'empty' ? (
        <Text style={styles.feedbackText}>“{displayedState.query}” için sonuç bulunamadı.</Text>
      ) : null}

      {displayedState.status === 'error' ? (
        <View style={styles.errorState}>
          <Text style={styles.errorTitle}>Yiyecekler yüklenemedi.</Text>
          <Pressable accessibilityRole="button" onPress={retry} style={styles.retryButton}>
            <Text style={styles.retryButtonText}>Tekrar Dene</Text>
          </Pressable>
        </View>
      ) : null}

      {displayedState.status === 'success' ? (
        <FlatList
          contentContainerStyle={styles.results}
          data={displayedState.items}
          keyboardShouldPersistTaps="handled"
          keyExtractor={(item) => String(item.foodId)}
          renderItem={({ item }) => <FoodSearchResultRow item={item} />}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, backgroundColor: '#f7faf8' },
  title: { color: '#1d2b26', fontSize: 28, fontWeight: '700' },
  input: {
    marginTop: 18,
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 14,
    paddingHorizontal: 16,
    paddingVertical: 14,
    backgroundColor: '#ffffff',
    color: '#1d2b26',
    fontSize: 17,
  },
  feedbackText: { marginTop: 20, color: '#64716c', fontSize: 15, lineHeight: 22 },
  loadingState: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  errorState: { alignItems: 'flex-start', gap: 14, marginTop: 22 },
  errorTitle: { color: '#7a3028', fontSize: 16, fontWeight: '700' },
  retryButton: {
    borderRadius: 10,
    paddingHorizontal: 15,
    paddingVertical: 11,
    backgroundColor: '#e6f2ed',
  },
  retryButtonText: { color: '#1f664f', fontWeight: '700' },
  results: { paddingTop: 18, paddingBottom: 28, gap: 10 },
  resultRow: {
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 14,
    padding: 16,
    backgroundColor: '#ffffff',
  },
  resultRowPressed: { opacity: 0.75 },
  resultName: { color: '#1d2b26', fontSize: 17, fontWeight: '700' },
  canonicalName: { marginTop: 5, color: '#64716c', fontSize: 14 },
  brand: { marginTop: 7, color: '#28785f', fontSize: 14, fontWeight: '600' },
});
