import { Redirect, useSegments } from 'expo-router';
import { View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { spacing, useTheme } from '@shared/ui/theme';

import { useSession } from '../hooks/useSession';

// Splash while we don't know yet; redirect into / out of the (auth) group
// based on session state. Per ADR-0006 / spec AC#6.
//
// AIDEV-NOTE: We check `useSegments()[0] === '(auth)'` so we don't redirect
// signed-out users to /sign-in WHEN THEY'RE ALREADY ON /sign-in — without
// this guard, AuthGate wraps the (auth) route too, re-evaluates after the
// Redirect mounts, sees signed-out again, redirects again, ad infinitum.
export function AuthGate({ children }: { children: React.ReactNode }) {
  const session = useSession();
  const segments = useSegments();
  const inAuthGroup = segments[0] === '(auth)';
  // AIDEV-NOTE: password-recovery establishes a real (signed-in) session, but
  // the user must still reach the top-level set-new-password screen. Let that
  // route through regardless of session so the signed-in→/library redirect
  // below doesn't bounce them off the recovery screen.
  const onRecoveryRoute = segments[0] === 'reset-password';

  if (session.status === 'loading') {
    return <AuthSplash />;
  }

  if (onRecoveryRoute) {
    return <>{children}</>;
  }

  if (session.status === 'signed-out' && !inAuthGroup) {
    return <Redirect href="/sign-in" />;
  }

  if (session.status === 'signed-in' && inAuthGroup) {
    return <Redirect href="/library" />;
  }

  return <>{children}</>;
}

function AuthSplash() {
  const theme = useTheme();
  return (
    <View
      testID="auth-splash"
      style={{
        flex: 1,
        alignItems: 'center',
        justifyContent: 'center',
        gap: spacing.md,
        backgroundColor: theme.color.canvas,
      }}
    >
      <Wordmark size={44} />
      <Text variant="label" tone="tertiary">
        Loading…
      </Text>
    </View>
  );
}
