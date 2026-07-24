import { Stack } from 'expo-router';

import { ScreenBoundary } from '@shared/ui/ScreenBoundary';

// Nested player stack. The root layout presents this whole group as a
// fullScreenModal (slide-up); `index` is the full player and `queue` slides up
// over it (FullPlayer routes here via router.push('/player/queue')). Without
// this layout, expo-router exposes flat `player/index` + `player/queue` routes
// and the root's <Stack.Screen name="player"> matches nothing.
//
// Wrapped in a ScreenBoundary for the same reason each tab stack is: the player
// is a route group of its own, so without one a render throw in FullPlayer or
// QueueSheet escapes to the root and takes down the whole app.
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
