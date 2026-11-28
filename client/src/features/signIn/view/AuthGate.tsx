import { useSessionExpired, useSession } from '@shared/auth';
import { AppRedirect, useRouteSegments } from '@shared/navigation';
import { Text, Wordmark } from '@shared/ui';
import { useRecoveryUnlocked } from '@features/signIn/view/useRecoveryUnlocked';
import { RESET_PASSWORD_ROUTE_SEGMENT } from '@features/signIn/core/authIntent';
import { useRecoveryUnlock } from '@features/signIn/view/SignInProvider';

import {
  AuthFullScreenNotice,
  InvalidRecoveryLinkNotice,
  SessionExpiredNotice,
} from '@features/signIn/view/AuthNotices';

export function AuthGate({ children }: { children: React.ReactNode }) {
  const session = useSession();
  const recovery = useRecoveryUnlock();
  const sessionExpired = useSessionExpired();
  const signedInUserId = session.status === 'signed-in' ? session.session.user.id : null;
  const recoveryUnlocked = useRecoveryUnlocked(recovery, signedInUserId);
  const segments = useRouteSegments();
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
    return <AppRedirect href="/sign-in" />;
  }

  if (session.status === 'signed-in' && sessionExpired && !inAuthGroup) {
    return <SessionExpiredNotice />;
  }

  if (session.status === 'signed-in' && inAuthGroup) {
    return <AppRedirect href="/library" />;
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
