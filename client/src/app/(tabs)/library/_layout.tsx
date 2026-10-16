import { Stack } from 'expo-router';

import { ScreenBoundary } from '@shared/ui/failure/ScreenBoundary';

export default function LibraryLayout() {
  return (
    <ScreenBoundary>
      <Stack screenOptions={{ headerShown: false }} />
    </ScreenBoundary>
  );
}
