import { Inter_400Regular, Inter_500Medium, Inter_600SemiBold } from '@expo-google-fonts/inter';
import {
  PlusJakartaSans_600SemiBold,
  PlusJakartaSans_700Bold,
} from '@expo-google-fonts/plus-jakarta-sans';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useFonts } from 'expo-font';
import { NavigationBar } from 'expo-navigation-bar';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { Platform } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { transientRetryOptions } from '../shared/query/retryDelay';
import { AuthGate } from '../features/auth/ui/AuthGate';
import { TestAuthBridge } from '../features/auth/ui/TestAuthBridge';
import { useAuthDeepLink } from '../features/auth/hooks/useAuthDeepLink';
import { useServerEvents } from '../shared/events/useServerEvents';
import { startKillSwitchPolling } from '../shared/killSwitch/killSwitchPoll';
import {
  PlaybackProvider,
  playsThroughTrackPlayer,
} from '../features/playback/hooks/PlaybackProvider';
import { SleepTimerBridge } from '../features/playback/ui/SleepTimerBridge';
import { OfflineReconcileBridge } from '../shared/offline/OfflineReconcileBridge';
import { ScreenBoundary } from '../shared/ui/ScreenBoundary';
import { ThemeProvider, themes } from '../shared/ui/theme';
import { useThemePreference } from '../shared/ui/theme/themePreference';

if (playsThroughTrackPlayer) {
  require('../features/playback/registerPlaybackService').registerPlaybackService();
}

void SplashScreen.preventAutoHideAsync();

// App-lifetime poll of the remote kill switches for the SSE, telemetry and offline-download loops.
startKillSwitchPolling();

function ServerEventsBridge() {
  useServerEvents();
  return null;
}

function AuthDeepLinkBridge() {
  useAuthDeepLink();
  return null;
}

export default function RootLayout() {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            ...transientRetryOptions,
          },
          // No global mutations.retry: many mutations are non-idempotent POSTs, so each
          // hook that is safe to repeat opts into isRetryable() itself (#841).
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
            {Platform.OS === 'android' && (
              <NavigationBar style={scheme === 'dark' ? 'light' : 'dark'} />
            )}
            <TestAuthBridge />
            <AuthGate>
              <ServerEventsBridge />
              <AuthDeepLinkBridge />
              <PlaybackProvider>
                <SleepTimerBridge />
                <OfflineReconcileBridge />
                <ScreenBoundary>
                  <Stack
                    screenOptions={{
                      headerShown: false,
                      contentStyle: { backgroundColor: activeTheme.color.canvas },
                    }}
                  >
                    <Stack.Screen name="(tabs)" />
                    <Stack.Screen name="(auth)" />
                    <Stack.Screen name="reset-password" />
                    <Stack.Screen
                      name="player"
                      options={{
                        presentation: 'fullScreenModal',
                        animation: 'slide_from_bottom',
                        gestureEnabled: true,
                      }}
                    />
                  </Stack>
                </ScreenBoundary>
              </PlaybackProvider>
            </AuthGate>
          </SafeAreaProvider>
        </ThemeProvider>
      </QueryClientProvider>
    </GestureHandlerRootView>
  );
}
