import '@/mobile/installDeviceFileStore';
import { Inter_400Regular, Inter_500Medium, Inter_600SemiBold } from '@expo-google-fonts/inter';
import {
  PlusJakartaSans_600SemiBold,
  PlusJakartaSans_700Bold,
} from '@expo-google-fonts/plus-jakarta-sans';
import { QueryClientProvider } from '@tanstack/react-query';
import { Asset } from 'expo-asset';
import { useFonts } from 'expo-font';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState, type ReactNode } from 'react';
import { Platform } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { installApiBase } from '@shared/apiConfig';
import { installAuthClient } from '@shared/auth';
import { installBuild } from '@shared/device';
import { createQueryClient } from '@shared/query';
import { appAuthParts, createAuthClient } from '@/mobile/authClient';
import { StagingBanner } from '@/mobile/frame/StagingBanner';
import { useNavigator } from '@shared/navigation';
import {
  AuthGate,
  AuthPortProvider,
  httpAuthPort,
  nativeLinkEvents,
  startAppAuthDeepLinks,
} from '@features/signIn';
import { registerSearchStateSignOutReset } from '@features/discovery';
import {
  createAppTelemetry,
  followSignedInUser,
  installGlobalErrorReporting,
  installTelemetry,
} from '@shared/telemetry';
import {
  installArtworkPlaceholder,
  NativePlaybackProvider,
  PlaybackProvider,
  SleepTimerBridge,
  startPlaybackService,
  trackPlayerPlayback,
} from '@features/player';
import { OfflineReconcileBridge, provideOfflinePort, usePinnedStore } from '@shared/offline';
import {
  useWideWebLayout,
  ScreenBoundary,
  TapFeedbackProvider,
  ThemeProvider,
  tapFeedback,
  themes,
  useThemePreference,
} from '@shared/ui';
import { AppChrome } from '@/mobile/frame/AppChrome';
import { FeaturePorts } from '@/mobile/wiring/FeaturePorts';
import { httpFeaturePorts, httpOfflinePort } from '@/mobile/wiring/httpFeaturePorts';
import { PlaybackShortcuts, SystemNavigationBar } from '@/mobile/frame/platformExtras';
import { reportOtaLaunchFailures } from '@/mobile/otaLaunchReport';
import { ScreenViewsBridge, ServerEventsBridge } from '@/mobile/serverEvents';
import { chainUncaughtErrorHandler, runtimeErrorHandlers } from '@/mobile/uncaughtErrors';
import {
  chainWindowUncaughtErrors,
  chainWindowUnhandledRejections,
  runtimeWindow,
} from '@/mobile/webErrorListeners';

installApiBase(process.env.EXPO_PUBLIC_API_URL, __DEV__);
installBuild({ platform: Platform.OS, commit: process.env.EXPO_PUBLIC_BUILD_COMMIT });
installAuthClient(
  createAuthClient(
    {
      url: process.env.EXPO_PUBLIC_SUPABASE_URL,
      anonKey: process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY,
    },
    appAuthParts(),
  ),
);
provideOfflinePort(httpOfflinePort);

installArtworkPlaceholder(() => Asset.fromModule(require('@assets/artwork-placeholder.png')).uri);
startPlaybackService();

registerSearchStateSignOutReset();

void SplashScreen.preventAutoHideAsync();

installTelemetry(createAppTelemetry());
followSignedInUser();
const globalWindow = runtimeWindow();
const chainRuntimeErrors = chainUncaughtErrorHandler(runtimeErrorHandlers());
const chainWindowErrors = chainWindowUncaughtErrors(globalWindow);
installGlobalErrorReporting((onError) => {
  chainRuntimeErrors(onError);
  chainWindowErrors(onError);
}, chainWindowUnhandledRejections(globalWindow));
reportOtaLaunchFailures().catch((error: unknown) => {
  console.warn('[telemetry] reading update logs failed', error);
});

function ConnectedOfflineReconcile() {
  const reconcile = usePinnedStore((state) => state.reconcile);
  return <OfflineReconcileBridge reconcile={reconcile} />;
}

function AuthDeepLinkBridge() {
  const router = useNavigator();

  useEffect(() => startAppAuthDeepLinks(nativeLinkEvents, router), [router]);
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

interface PlayerProvidersProps {
  children: ReactNode;
}

function PlayerProviders({ children }: PlayerProvidersProps) {
  return (
    <NativePlaybackProvider port={trackPlayerPlayback}>
      <PlaybackProvider>{children}</PlaybackProvider>
    </NativePlaybackProvider>
  );
}

export default function RootLayout() {
  const isWideWeb = useWideWebLayout();
  const [queryClient] = useState(createQueryClient);

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
            <TapFeedbackProvider feedback={tapFeedback}>
              <ThemeProvider>
                <SafeAreaProvider>
                  <StatusBar style={scheme === 'dark' ? 'light' : 'dark'} />
                  <SystemNavigationBar scheme={scheme} />
                  <AuthDeepLinkBridge />
                  <StagingBanner supabaseUrl={process.env.EXPO_PUBLIC_SUPABASE_URL} />
                  <AuthGate>
                    <ServerEventsBridge />
                    <ScreenViewsBridge />
                    <PlayerProviders>
                      <SleepTimerBridge />
                      <ConnectedOfflineReconcile />
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
                    </PlayerProviders>
                  </AuthGate>
                </SafeAreaProvider>
              </ThemeProvider>
            </TapFeedbackProvider>
          </FeaturePorts>
        </AuthPortProvider>
      </QueryClientProvider>
    </GestureHandlerRootView>
  );
}
