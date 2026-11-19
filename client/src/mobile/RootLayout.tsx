import '@/mobile/installDeviceFileStore';
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

import { installAuthClient } from '@shared/auth';
import { createQueryClient } from '@shared/query';
import { createAuthClient } from '@/mobile/authClient';
import { StagingBanner } from '@/mobile/frame/StagingBanner';
import {
  AuthGate,
  AuthPortProvider,
  httpAuthPort,
  useAuthDeepLink,
} from '@features/signIn';
import { registerSearchStateSignOutReset } from '@features/discovery';
import { followSignedInUser, installGlobalErrorReporting } from '@shared/telemetry';
import { PlaybackProvider, SleepTimerBridge, startPlaybackService } from '@features/player';
import { OfflineReconcileBridge, provideOfflinePort } from '@shared/offline';
import {
  useWideWebLayout,
  ScreenBoundary,
  ThemeProvider,
  themes,
  useThemePreference,
} from '@shared/ui';
import { AppChrome } from '@/mobile/frame/AppChrome';
import { startKillSwitchPolling } from '@/mobile/killSwitchPoll';
import { FeaturePorts } from '@/mobile/wiring/FeaturePorts';
import { httpFeaturePorts, httpOfflinePort } from '@/mobile/wiring/httpFeaturePorts';
import { PlaybackShortcuts, SystemNavigationBar } from '@/mobile/frame/platformExtras';
import { reportOtaLaunchFailures } from '@/mobile/otaLaunchReport';
import { ScreenViewsBridge, ServerEventsBridge } from '@/mobile/serverEvents';
import { chainUncaughtErrorHandler } from '@/mobile/uncaughtErrors';

installAuthClient(createAuthClient());
provideOfflinePort(httpOfflinePort);

startPlaybackService();

registerSearchStateSignOutReset();

void SplashScreen.preventAutoHideAsync();

followSignedInUser();
installGlobalErrorReporting(chainUncaughtErrorHandler);
reportOtaLaunchFailures().catch((error: unknown) => {
  console.warn('[telemetry] reading update logs failed', error);
});

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
