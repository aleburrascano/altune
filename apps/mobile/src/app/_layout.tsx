import { Inter_400Regular, Inter_500Medium, Inter_600SemiBold } from '@expo-google-fonts/inter';
import { SpaceGrotesk_500Medium, SpaceGrotesk_600SemiBold } from '@expo-google-fonts/space-grotesk';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useFonts } from 'expo-font';
import { Slot } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { AuthGate } from '../features/auth/ui/AuthGate';
import { ThemeProvider } from '../shared/ui/theme';

// AIDEV-NOTE: ADR-0005 — single QueryClientProvider at the Expo Router root.
// Every feature's hooks (useLibrary, etc.) inherit this client.
// ADR-0006 — root is auth-aware via AuthGate: splash while loading, redirect
// to /sign-in when signed-out, mount the app tree when signed-in.
// ADR-0008 — ThemeProvider wraps the tree (dark is the only v1 mode); the
// design-system fonts (Space Grotesk + Inter) are held behind the native
// splash until loaded so the UI never flashes a fallback font (FOUT).
void SplashScreen.preventAutoHideAsync();

export default function RootLayout() {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 30_000 } } }),
  );

  const [fontsLoaded, fontError] = useFonts({
    SpaceGrotesk_500Medium,
    SpaceGrotesk_600SemiBold,
    Inter_400Regular,
    Inter_500Medium,
    Inter_600SemiBold,
  });

  useEffect(() => {
    if (fontsLoaded || fontError) {
      void SplashScreen.hideAsync();
    }
  }, [fontsLoaded, fontError]);

  if (!fontsLoaded && !fontError) {
    return null;
  }

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <SafeAreaProvider>
          <StatusBar style="light" />
          <AuthGate>
            <Slot />
          </AuthGate>
        </SafeAreaProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
}
