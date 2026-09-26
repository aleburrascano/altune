import { Stack, useRouter } from 'expo-router';

import { useIsWideWebLayout } from '@shared/ui/layout/useLayoutMode';
import { ScreenBoundary } from '@shared/ui/ScreenBoundary';
import type { TabRoute } from '@shared/ui/navigation/tabRoutes';
import { WideChrome } from '@/wideChrome';

function modalOptions(isWideWeb: boolean) {
  return isWideWeb ? {} : { presentation: 'modal' as const, animation: 'slide_from_bottom' as const, gestureEnabled: true };
}

export default function PlayerLayout() {
  const isWideWeb = useIsWideWebLayout();
  const router = useRouter();

  const stack = (
    <ScreenBoundary>
      <Stack screenOptions={{ headerShown: false }}>
        <Stack.Screen name="index" />
        <Stack.Screen name="queue" options={modalOptions(isWideWeb)} />
        <Stack.Screen name="lyrics" options={modalOptions(isWideWeb)} />
      </Stack>
    </ScreenBoundary>
  );

  if (!isWideWeb) return stack;

  return (
    <WideChrome activeRoute="" onNavigate={(route: TabRoute) => router.push(`/${route}`)}>
      {stack}
    </WideChrome>
  );
}
