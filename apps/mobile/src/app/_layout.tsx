import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Slot } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useState } from 'react';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { AuthGate } from '../features/auth/ui/AuthGate';

// AIDEV-NOTE: ADR-0005 — single QueryClientProvider at the Expo Router root.
// Every feature's hooks (useLibrary, etc.) inherit this client.
// ADR-0006 — root is auth-aware via AuthGate: splash while loading, redirect
// to /sign-in when signed-out, mount the app tree when signed-in.

export default function RootLayout() {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 30_000 } } }),
  );

  return (
    <QueryClientProvider client={queryClient}>
      <SafeAreaProvider>
        <StatusBar style="auto" />
        <AuthGate>
          <Slot />
        </AuthGate>
      </SafeAreaProvider>
    </QueryClientProvider>
  );
}
