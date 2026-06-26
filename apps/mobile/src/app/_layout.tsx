import { Inter_400Regular, Inter_500Medium, Inter_600SemiBold } from '@expo-google-fonts/inter';
import {
  PlusJakartaSans_600SemiBold,
  PlusJakartaSans_700Bold,
} from '@expo-google-fonts/plus-jakarta-sans';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useFonts } from 'expo-font';
import * as NavigationBar from 'expo-navigation-bar';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { AppState, Platform } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { AuthGate } from '../features/auth/ui/AuthGate';
import { useAuthDeepLink } from '../features/auth/hooks/useAuthDeepLink';
import { useServerEvents } from '../shared/events/useServerEvents';
import { PlaybackProvider } from '../features/playback/hooks/PlaybackProvider';
import { isExpoGo } from '../shared/playback/isExpoGo';
import { ThemeProvider, darkTheme } from '../shared/ui/theme';

// Registering the playback service pulls in react-native-track-player's native
// module, which Expo Go does not bundle — skip it there so the app boots.
if (!isExpoGo) {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  require('../features/playback/registerPlaybackService').registerPlaybackService();
}

// AIDEV-NOTE: ADR-0005 — single QueryClientProvider at the Expo Router root.
// Every feature's hooks (useLibrary, etc.) inherit this client.
// ADR-0006 — root is auth-aware via AuthGate: splash while loading, redirect
// to /sign-in when signed-out, mount the app tree when signed-in.
// ADR-0008 — ThemeProvider wraps the tree (dark is the only v1 mode); the
// design-system fonts (Space Grotesk + Inter) are held behind the native
// splash until loaded so the UI never flashes a fallback font (FOUT).
void SplashScreen.preventAutoHideAsync();

function ServerEventsBridge() {
  useServerEvents();
  return null;
}

// AIDEV-NOTE: the auth deep-link spine (email-confirm / recovery / OAuth
// callbacks). Mounted inside AuthGate next to ServerEventsBridge; cold-start
// links survive via Linking.getInitialURL even across the sign-in redirect.
function AuthDeepLinkBridge() {
  useAuthDeepLink();
  return null;
}

export default function RootLayout() {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 30_000 } } }),
  );

  const [fontsLoaded, fontError] = useFonts({
    PlusJakartaSans_600SemiBold,
    PlusJakartaSans_700Bold,
    Inter_400Regular,
    Inter_500Medium,
    Inter_600SemiBold,
  });

  useEffect(() => {
    if (fontsLoaded || fontError) {
      void SplashScreen.hideAsync();
    }
  }, [fontsLoaded, fontError]);

  // AIDEV-NOTE: Android paints the OS navigation bar light by default and
  // repaints it on resume (SDK 54 edge-to-edge), flashing white over our dark
  // UI. Force dark + light buttons on mount AND every time the app returns to
  // the foreground — the re-assert on 'active' is what kills the resume flash.
  useEffect(() => {
    if (Platform.OS !== 'android') {
      return;
    }
    const applyDarkNavBar = (): void => {
      void NavigationBar.setBackgroundColorAsync(darkTheme.color.canvas);
      void NavigationBar.setButtonStyleAsync('light');
    };
    applyDarkNavBar();
    const sub = AppState.addEventListener('change', (next) => {
      if (next === 'active') {
        applyDarkNavBar();
      }
    });
    return () => sub.remove();
  }, []);

  if (!fontsLoaded && !fontError) {
    return null;
  }

  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <SafeAreaProvider>
            <StatusBar style="light" />
            <AuthGate>
              <ServerEventsBridge />
              <AuthDeepLinkBridge />
              <PlaybackProvider>
                <Stack screenOptions={{ headerShown: false }}>
                  <Stack.Screen name="(tabs)" />
                  <Stack.Screen name="(auth)" />
                  <Stack.Screen name="reset-password" />
                  <Stack.Screen name="player" options={{ presentation: 'fullScreenModal', animation: 'slide_from_bottom', gestureEnabled: true }} />
                </Stack>
              </PlaybackProvider>
            </AuthGate>
          </SafeAreaProvider>
        </ThemeProvider>
      </QueryClientProvider>
    </GestureHandlerRootView>
  );
}
