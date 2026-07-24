import { Stack } from 'expo-router';
import { View } from 'react-native';

import { ArtworkBackground } from '@features/auth/ui/hero/ArtworkBackground';
import { ScreenBoundary } from '@shared/ui/ScreenBoundary';
import { themes } from '@shared/ui/theme';
import { useThemePreference } from '@shared/ui/theme/themePreference';

// AIDEV-NOTE: The (auth) route group lives OUTSIDE the AuthGate's redirect
// scope so signed-out users can reach /sign-in and /sign-up.
//
// The artwork background is drawn ONCE here, behind a transparent Stack with
// cross-fade transitions, so navigating sign-in <-> sign-up <-> forgot never
// remounts the blurred wall (which caused a bright "unblurred" flash mid-
// transition). Screens render with background={false} over this persistent one.
//
// The boundary wraps only the Stack, so a crash in a sign-in/sign-up screen
// degrades to the retry fallback instead of an unrecoverable blank app — the
// one route group a signed-out user cannot navigate away from.

export default function AuthLayout() {
  const scheme = useThemePreference((s) => s.scheme);
  return (
    <View style={{ flex: 1, backgroundColor: themes[scheme].color.canvas }}>
      <ArtworkBackground />
      <ScreenBoundary>
        <Stack
          screenOptions={{
            headerShown: false,
            animation: 'fade',
            contentStyle: { backgroundColor: 'transparent' },
          }}
        />
      </ScreenBoundary>
    </View>
  );
}
