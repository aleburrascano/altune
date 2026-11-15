import { Stack } from 'expo-router';

import { ScreenBoundary } from '@shared/ui';

export default function DiscoverLayout() {
  return (
    <ScreenBoundary>
      <Stack screenOptions={{ headerShown: false }} />
    </ScreenBoundary>
  );
}
