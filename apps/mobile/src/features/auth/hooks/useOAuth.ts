import { NetworkError } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';
import { useNavigator } from '@shared/navigation';

import { withAuthDeadline } from '../authDeadline';
import { completeAuthIntent, type AuthRouter } from '../completeAuthIntent';
import { openAuthSession } from '../native/authBrowser';
import {
  failureReason,
  OAUTH_BROWSER_TIMEOUT_MS,
  thrownFailure,
  useOAuthFlow,
  type OAuthFailure,
  type OAuthOutcome,
  type OAuthProvider,
} from '../oauthRequest';
import { authRedirectUrl, parseAuthLink } from '../parseAuthLink';

export { OAUTH_BROWSER_TIMEOUT_MS } from '../oauthRequest';

type AuthorizationRequest = { kind: 'authorization_url'; url: string } | OAuthFailure;

async function requestAuthorizationUrl(provider: OAuthProvider): Promise<AuthorizationRequest> {
  const { data, error } = await withAuthDeadline(
    supabase.auth.signInWithOAuth({
      provider,
      options: { redirectTo: authRedirectUrl('callback'), skipBrowserRedirect: true },
    }),
  );
  if (error) return { kind: 'error', reason: failureReason(error) };
  if (!data?.url) return { kind: 'error', reason: 'unknown' };
  return { kind: 'authorization_url', url: data.url };
}

async function redirectFromBrowser(authorizationUrl: string): Promise<string | null> {
  let session: Awaited<ReturnType<typeof openAuthSession>>;
  try {
    session = await withAuthDeadline(
      openAuthSession(authorizationUrl, authRedirectUrl('callback')),
      OAUTH_BROWSER_TIMEOUT_MS,
    );
  } catch (err) {
    if (err instanceof NetworkError && err.failure === 'timeout') return null;
    throw err;
  }
  return session.type === 'success' && session.url ? session.url : null;
}

async function exchangeRedirect(redirectUrl: string, router: AuthRouter): Promise<OAuthOutcome> {
  const outcome = await withAuthDeadline(
    completeAuthIntent(parseAuthLink(redirectUrl), router, supabase.auth),
  );
  const exchanged = outcome.kind === 'success' || outcome.kind === 'deduped';
  if (exchanged) return { kind: 'ok' };
  const reason =
    outcome.kind === 'failure' && outcome.error ? failureReason(outcome.error) : 'unknown';
  return { kind: 'error', reason };
}

async function signInOutcome(provider: OAuthProvider, router: AuthRouter): Promise<OAuthOutcome> {
  try {
    const authorization = await requestAuthorizationUrl(provider);
    if (authorization.kind !== 'authorization_url') return authorization;
    const redirectUrl = await redirectFromBrowser(authorization.url);
    if (!redirectUrl) return { kind: 'cancelled' };
    return await exchangeRedirect(redirectUrl, router);
  } catch (err) {
    return thrownFailure(err);
  }
}

export function useOAuth() {
  const navigator = useNavigator();
  return useOAuthFlow((provider) => signInOutcome(provider, navigator));
}
