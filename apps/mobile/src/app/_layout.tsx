import { Inter_400Regular, Inter_500Medium, Inter_600SemiBold } from '@expo-google-fonts/inter';
import {
  PlusJakartaSans_600SemiBold,
  PlusJakartaSans_700Bold,
} from '@expo-google-fonts/plus-jakarta-sans';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useFonts } from 'expo-font';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { appMutationCache, appQueryCache } from '../shared/query/useAppMutation';
import { transientRetryOptions } from '../shared/query/retryDelay';
import { AuthGate } from '../features/auth/ui/AuthGate';
import { TestAuthBridge } from '../features/auth/ui/TestAuthBridge';
import { useAuthDeepLink } from '../features/auth/hooks/useAuthDeepLink';
import { useServerEvents } from '../shared/events/useServerEvents';
import { startKillSwitchPolling } from '../shared/killSwitch/killSwitchPoll';
import { installGlobalErrorReporting } from '../shared/telemetry/clientErrorReporting';
import { PlaybackProvider } from '../features/playback/hooks/PlaybackProvider';
import { playsThroughTrackPlayer } from '../features/playback/playsThroughTrackPlayer';
import { SleepTimerBridge } from '../features/playback/ui/SleepTimerBridge';
import { OfflineReconcileBridge } from '../shared/offline/OfflineReconcileBridge';
import { useWideWebLayout } from '../shared/ui/layout/useWideWebLayout';
import { ScreenBoundary } from '../shared/ui/ScreenBoundary';
import { ThemeProvider, themes } from '../shared/ui/theme';
import { useThemePreference } from '../shared/ui/theme/themePreference';
import { AppChrome } from '../app-shell/AppChrome';
import { PlaybackShortcuts, SystemNavigationBar } from '../app-shell/platformExtras';

if (playsThroughTrackPlayer) {
  require('../features/playback/native/registerPlaybackService').registerPlaybackService();
}

void SplashScreen.preventAutoHideAsync();

startKillSwitchPolling();

installGlobalErrorReporting();

function ServerEventsBridge() {
  useServerEvents();
  return null;
}

function AuthDeepLinkBridge() {
  useAuthDeepLink();
  return null;
}

function playerScreenOptions(isWideWeb: boolean) {
  return isWideWeb
    ? { animation: 'none' as const }
    : {
        presentation: 'fullScreenModal' as const,
        animation: 'slide_from_bottom' as const,
        gestureEnabled: true,
      };
}

export default function RootLayout() {
  const isWideWeb = useWideWebLayout();
  const [queryClient] = useState(
    () =>
      new QueryClient({
        queryCache: appQueryCache(),
        mutationCache: appMutationCache(),
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            ...transientRetryOptions,
          },
        },
      }),
  );

  const scheme = useThemePreference((s) => s.scheme);
  const activeTheme = themes[scheme];

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

  if (!fontsLoaded && !fontError) {
    return null;
  }

  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <SafeAreaProvider>
            <StatusBar style={scheme === 'dark' ? 'light' : 'dark'} />
            <SystemNavigationBar scheme={scheme} />
            <TestAuthBridge />
            <AuthDeepLinkBridge />
            <AuthGate>
              <ServerEventsBridge />
              <PlaybackProvider>
                <SleepTimerBridge />
                <OfflineReconcileBridge />
                <PlaybackShortcuts />
                <ScreenBoundary>
                  <AppChrome isWideWeb={isWideWeb}>
                    <Stack
                      screenOptions={{
                        headerShown: false,
                        contentStyle: { backgroundColor: activeTheme.color.canvas },
                      }}
                    >
                      <Stack.Screen name="(tabs)" />
                      <Stack.Screen name="(auth)" />
                      <Stack.Screen name="reset-password" />
                      <Stack.Screen name="player" options={playerScreenOptions(isWideWeb)} />
                    </Stack>
                  </AppChrome>
                </ScreenBoundary>
              </PlaybackProvider>
            </AuthGate>
          </SafeAreaProvider>
        </ThemeProvider>
      </QueryClientProvider>
    </GestureHandlerRootView>
  );
}
