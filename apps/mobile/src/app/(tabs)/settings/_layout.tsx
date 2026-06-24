import { Stack } from 'expo-router';

import { ScreenBoundary } from '@shared/ui/ScreenBoundary';

export default function SettingsLayout() {
  return (
    <ScreenBoundary>
      <Stack screenOptions={{ headerShown: false }} />
    </ScreenBoundary>
  );
}
