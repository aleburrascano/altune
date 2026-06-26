import { Stack } from 'expo-router';

import { darkTheme } from '@shared/ui/theme';

// AIDEV-NOTE: The (auth) route group lives OUTSIDE the AuthGate's redirect
// scope so signed-out users can reach /sign-in and /sign-up. Slice 10 stubs
// the screens; real forms land in Slices 11/12.
//
// contentStyle paints the card canvas-dark so navigating between auth screens
// doesn't flash the default white stack background.

export default function AuthLayout() {
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        contentStyle: { backgroundColor: darkTheme.color.canvas },
      }}
    />
  );
}
