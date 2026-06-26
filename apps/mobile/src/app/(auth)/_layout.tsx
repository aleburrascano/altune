import { Stack } from 'expo-router';
import { View } from 'react-native';

import { ArtworkBackground } from '@features/auth/ui/hero/ArtworkBackground';
import { darkTheme } from '@shared/ui/theme';

// AIDEV-NOTE: The (auth) route group lives OUTSIDE the AuthGate's redirect
// scope so signed-out users can reach /sign-in and /sign-up.
//
// The artwork background is drawn ONCE here, behind a transparent Stack with
// cross-fade transitions, so navigating sign-in <-> sign-up <-> forgot never
// remounts the blurred wall (which caused a bright "unblurred" flash mid-
// transition). Screens render with background={false} over this persistent one.

export default function AuthLayout() {
  return (
    <View style={{ flex: 1, backgroundColor: darkTheme.color.canvas }}>
      <ArtworkBackground />
      <Stack
        screenOptions={{
          headerShown: false,
          animation: 'fade',
          contentStyle: { backgroundColor: 'transparent' },
        }}
      />
    </View>
  );
}
