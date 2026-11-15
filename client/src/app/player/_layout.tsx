import { Stack } from 'expo-router';

import { useWideWebLayout, ScreenBoundary } from '@shared/ui';

function modalOptions(isWideWeb: boolean) {
  return isWideWeb
    ? {}
    : {
        presentation: 'modal' as const,
        animation: 'slide_from_bottom' as const,
        gestureEnabled: true,
      };
}

export default function PlayerLayout() {
  const isWideWeb = useWideWebLayout();

  return (
    <ScreenBoundary>
      <Stack screenOptions={{ headerShown: false }}>
        <Stack.Screen name="index" />
        <Stack.Screen name="queue" options={modalOptions(isWideWeb)} />
        <Stack.Screen name="lyrics" options={modalOptions(isWideWeb)} />
      </Stack>
    </ScreenBoundary>
  );
}
