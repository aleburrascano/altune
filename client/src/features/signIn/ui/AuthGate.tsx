import { Redirect, useSegments } from 'expo-router';

import { useSessionExpired, useSession } from '@shared/auth';
import { Text, Wordmark } from '@shared/ui';
import { useRecoveryUnlocked } from '@features/signIn/view/useRecoveryUnlocked';
import {
  RESET_PASSWORD_ROUTE_SEGMENT,
  recoveryUnlock,
} from '@features/signIn/shell/passwordRecovery';

import {
  AuthFullScreenNotice,
  InvalidRecoveryLinkNotice,
  SessionExpiredNotice,
} from '@features/signIn/ui/AuthNotices';

export function AuthGate({ children }: { children: React.ReactNode }) {
  const session = useSession();
  const sessionExpired = useSessionExpired();
  const signedInUserId = session.status === 'signed-in' ? session.session.user.id : null;
  const recoveryUnlocked = useRecoveryUnlocked(recoveryUnlock, signedInUserId);
  const segments = useSegments();
  const inAuthGroup = segments[0] === '(auth)';
  const onRecoveryRoute = segments[0] === RESET_PASSWORD_ROUTE_SEGMENT;
  const onAuthCallbackRoute = segments[0] === 'auth';

  if (session.status === 'loading') {
    return <AuthSplash />;
  }

  if (onRecoveryRoute) {
    return recoveryUnlocked ? <>{children}</> : <InvalidRecoveryLinkNotice />;
  }

  if (session.status === 'signed-out' && !inAuthGroup && !onAuthCallbackRoute) {
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
  return (
    <AuthFullScreenNotice testID="auth-splash">
      <Wordmark size={44} />
      <Text variant="label" tone="tertiary">
        Loading…
      </Text>
    </AuthFullScreenNotice>
  );
}
