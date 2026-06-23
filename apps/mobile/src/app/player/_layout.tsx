import { Stack } from 'expo-router';

// Nested player stack. The root layout presents this whole group as a
// fullScreenModal (slide-up); `index` is the full player and `queue` slides up
// over it (FullPlayer routes here via router.push('/player/queue')). Without
// this layout, expo-router exposes flat `player/index` + `player/queue` routes
// and the root's <Stack.Screen name="player"> matches nothing.
export default function PlayerLayout() {
  return (
    <Stack screenOptions={{ headerShown: false }}>
      <Stack.Screen name="index" />
      <Stack.Screen
        name="queue"
        options={{ presentation: 'modal', animation: 'slide_from_bottom', gestureEnabled: true }}
      />
    </Stack>
  );
}
