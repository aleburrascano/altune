import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useState } from 'react';
import { SafeAreaProvider } from 'react-native-safe-area-context';

// AIDEV-NOTE: ADR-0005 — single QueryClientProvider at the Expo Router root.
// Every feature's hooks (useLibrary, etc.) inherit this client.

export default function RootLayout() {
  // useState ensures a single QueryClient instance survives re-renders;
  // creating it inline would re-instantiate on every render and trash the cache.
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 30_000 } } })
  );

  return (
    <QueryClientProvider client={queryClient}>
      <SafeAreaProvider>
        <StatusBar style="auto" />
        <Stack screenOptions={{ headerShown: false }} />
      </SafeAreaProvider>
    </QueryClientProvider>
  );
}
