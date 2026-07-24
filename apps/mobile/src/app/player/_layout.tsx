import { Stack } from 'expo-router';

import { ScreenBoundary } from '@shared/ui/ScreenBoundary';

export default function PlayerLayout() {
  return (
    <ScreenBoundary>
      <Stack screenOptions={{ headerShown: false }}>
        <Stack.Screen name="index" />
        <Stack.Screen
          name="queue"
          options={{ presentation: 'modal', animation: 'slide_from_bottom', gestureEnabled: true }}
        />
        <Stack.Screen
          name="lyrics"
          options={{ presentation: 'modal', animation: 'slide_from_bottom', gestureEnabled: true }}
        />
      </Stack>
    </ScreenBoundary>
  );
}
