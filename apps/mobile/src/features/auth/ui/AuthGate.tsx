import { Redirect, useSegments } from 'expo-router';
import { View } from 'react-native';

import { useSessionExpired } from '@shared/auth/sessionExpired';
import { useSession } from '@shared/auth/useSession';
import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { spacing, useTheme } from '@shared/ui/theme';

import { useRecoveryUnlocked } from '../recoveryUnlock';

import { InvalidRecoveryLinkNotice } from './InvalidRecoveryLinkNotice';
import { SessionExpiredNotice } from './SessionExpiredNotice';

export function AuthGate({ children }: { children: React.ReactNode }) {
  const session = useSession();
  const sessionExpired = useSessionExpired();
  const recoveryUnlocked = useRecoveryUnlocked();
  const segments = useSegments();
  const inAuthGroup = segments[0] === '(auth)';
  const onRecoveryRoute = segments[0] === 'reset-password';

  if (session.status === 'loading') {
    return <AuthSplash />;
  }

  // The reset-password screen is reachable via the `altune` scheme, so the route
  // segment alone proves nothing. Only render it once a recovery exchange has
  // actually been verified (see #656); otherwise the link is bare or expired.
  if (onRecoveryRoute) {
    return recoveryUnlocked ? <>{children}</> : <InvalidRecoveryLinkNotice />;
  }

  if (session.status === 'signed-out' && !inAuthGroup) {
    return <Redirect href="/sign-in" />;
  }

  if (session.status === 'signed-in' && sessionExpired && !inAuthGroup) {
    return <SessionExpiredNotice />;
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
