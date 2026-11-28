import { startOAuth, withAuthDeadline } from '@shared/auth';
import {
  failureReason,
  thrownFailure,
  type OAuthFailure,
  type OAuthProvider,
} from '@features/signIn/core/oauthResult';
import { authRedirectUrl } from '@features/signIn/core/authRedirect';
import { useAuthPort } from '@features/signIn/view/SignInProvider';
import type { AuthPort } from '@features/signIn/ports/ports';

import { useOAuthFlow } from '@features/signIn/view/useOAuthFlow';

async function redirectToProvider(provider: OAuthProvider): Promise<OAuthFailure | null> {
  const { error } = await withAuthDeadline(
    startOAuth({
      provider,
      options: { redirectTo: authRedirectUrl('callback') },
    }),
  );
  return error ? { kind: 'error', reason: failureReason(error) } : null;
}

async function attemptWebRedirect(provider: OAuthProvider): Promise<OAuthFailure | null> {
  try {
    return await redirectToProvider(provider);
  } catch (err) {
    return thrownFailure(err);
  }
}

async function beginWebRedirect(
  authPort: AuthPort,
  provider: OAuthProvider,
): Promise<OAuthFailure | null> {
  const failure = await attemptWebRedirect(provider);
  if (failure) authPort.reportSignInFailure(failure.reason);
  return failure;
}

export function useOAuth() {
  const authPort = useAuthPort();
  return useOAuthFlow((provider) => beginWebRedirect(authPort, provider));
}
