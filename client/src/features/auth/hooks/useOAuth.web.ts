import * as WebBrowser from 'expo-web-browser';

import { startOAuth } from '@shared/auth/authActions';

import { withAuthDeadline } from '@shared/auth/authDeadline';
import {
  failureReason,
  thrownFailure,
  type OAuthFailure,
  type OAuthProvider,
} from '@features/auth/domain/oauthResult';
import { authRedirectUrl } from '@features/auth/domain/authRedirect';
import { useAuthPort } from '@features/auth/ports/AuthPortProvider';
import type { AuthPort } from '@features/auth/ports/authPort';

import { useOAuthFlow } from '@features/auth/hooks/useOAuthFlow';

WebBrowser.maybeCompleteAuthSession();

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
