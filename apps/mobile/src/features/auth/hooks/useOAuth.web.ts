import * as WebBrowser from 'expo-web-browser';

import { supabase } from '@shared/auth/supabaseClient';

import { withAuthDeadline } from '../authDeadline';
import {
  failureReason,
  thrownFailure,
  useOAuthFlow,
  type OAuthFailure,
  type OAuthProvider,
} from '../oauthRequest';
import { authRedirectUrl } from '../parseAuthLink';
import { reportSignInFailure } from '../reportSignInFailure';

WebBrowser.maybeCompleteAuthSession();

export { OAUTH_BROWSER_TIMEOUT_MS } from '../oauthRequest';

async function redirectToProvider(provider: OAuthProvider): Promise<OAuthFailure | null> {
  const { error } = await withAuthDeadline(
    supabase.auth.signInWithOAuth({
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

async function beginWebRedirect(provider: OAuthProvider): Promise<OAuthFailure | null> {
  const failure = await attemptWebRedirect(provider);
  if (failure) reportSignInFailure(failure.reason);
  return failure;
}

export function useOAuth() {
  return useOAuthFlow(beginWebRedirect);
}
