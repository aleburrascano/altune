import { Inter_400Regular, Inter_500Medium, Inter_600SemiBold } from '@expo-google-fonts/inter';
import {
  PlusJakartaSans_600SemiBold,
  PlusJakartaSans_700Bold,
} from '@expo-google-fonts/plus-jakarta-sans';
import { QueryClientProvider } from '@tanstack/react-query';
import { useFonts } from 'expo-font';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { createQueryClient } from '@shared/query/createQueryClient';
import { StagingBanner } from '@/shell/StagingBanner';
import {
  AuthGate,
  AuthPortProvider,
  httpAuthPort,
  TestAuthBridge,
  useAuthDeepLink,
} from '@features/auth';
import { registerSearchStateSignOutReset } from '@features/discovery';
import { useServerEvents } from '@shared/events/useServerEvents';
import { startKillSwitchPolling } from '@shared/killSwitch/killSwitchPoll';
import { useScreenViews } from '@shared/events/useScreenViews';
import { installGlobalErrorReporting } from '@shared/telemetry/clientErrorReporting';
import { PlaybackProvider, SleepTimerBridge, startPlaybackService } from '@features/playback';
import { OfflineReconcileBridge } from '@shared/offline/ui/OfflineReconcileBridge';
import { useWideWebLayout } from '@shared/ui/layout/useLayoutMode';
import { ScreenBoundary } from '@shared/ui/ScreenBoundary';
import { ThemeProvider, themes } from '@shared/ui/theme';
import { useThemePreference } from '@shared/ui/theme/themePreference';
import { AppChrome } from '@/shell/AppChrome';
import { FeaturePorts } from '@/wiring/FeaturePorts';
import { httpFeaturePorts } from '@/wiring/httpFeaturePorts';
import { PlaybackShortcuts, SystemNavigationBar } from '@/shell/platformExtras';

startPlaybackService();

registerSearchStateSignOutReset();

void SplashScreen.preventAutoHideAsync();

installGlobalErrorReporting();

function ServerEventsBridge() {
  useServerEvents();
  return null;
}

function ScreenViewsBridge() {
  useScreenViews();
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
  const [queryClient] = useState(createQueryClient);

  useEffect(() => startKillSwitchPolling(), []);

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
        <AuthPortProvider port={httpAuthPort}>
          <FeaturePorts ports={httpFeaturePorts}>
            <ThemeProvider>
              <SafeAreaProvider>
                <StatusBar style={scheme === 'dark' ? 'light' : 'dark'} />
                <SystemNavigationBar scheme={scheme} />
                <TestAuthBridge />
                <AuthDeepLinkBridge />
                <StagingBanner />
                <AuthGate>
                  <ServerEventsBridge />
                  <ScreenViewsBridge />
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
          </FeaturePorts>
        </AuthPortProvider>
      </QueryClientProvider>
    </GestureHandlerRootView>
  );
}
