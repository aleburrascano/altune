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

async function beginWebRedirect(provider: OAuthProvider): Promise<OAuthFailure | null> {
  try {
    return await redirectToProvider(provider);
  } catch (err) {
    return thrownFailure(err);
  }
}

export function useOAuth() {
  return useOAuthFlow(beginWebRedirect);
}
