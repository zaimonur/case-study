import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';

import { MealStoreProvider } from '../src/state/MealStoreProvider';

export default function RootLayout() {
  return (
    <MealStoreProvider>
      <StatusBar style="auto" />
      <Stack>
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen
          name="search"
          options={{ headerBackButtonDisplayMode: 'minimal', title: 'Yiyecek Ara' }}
        />
        <Stack.Screen name="food/[id]" options={{ title: 'Yiyecek Detayı' }} />
      </Stack>
    </MealStoreProvider>
  );
}
