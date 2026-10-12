import { NetworkError } from '@shared/failure/errors';
import { authIntentClient, startOAuth } from '@shared/auth/authActions';
import { useNavigator } from '@shared/navigation';

import { withAuthDeadline } from '@shared/auth/authDeadline';
import { authRedirectUrl } from '@features/auth/domain/authRedirect';
import { completeAuthIntent, type AuthRouter } from '@features/auth/domain/completeAuthIntent';
import { dismissAuthSession, openAuthSession } from '@features/auth/native/authBrowser';
import {
  failureReason,
  OAUTH_BROWSER_TIMEOUT_MS,
  thrownFailure,
  type OAuthFailure,
  type OAuthOutcome,
  type OAuthProvider,
} from '@features/auth/domain/oauthResult';
import { parseAuthLink } from '@features/auth/domain/parseAuthLink';
import { useAuthPort } from '@features/auth/ports/AuthPortProvider';
import type { AuthPort } from '@features/auth/ports/authPort';

import { useOAuthFlow } from '@features/auth/hooks/useOAuthFlow';

export { OAUTH_BROWSER_TIMEOUT_MS } from '@features/auth/domain/oauthResult';

type AuthorizationRequest = { kind: 'authorization_url'; url: string } | OAuthFailure;

async function requestAuthorizationUrl(provider: OAuthProvider): Promise<AuthorizationRequest> {
  const { data, error } = await withAuthDeadline(
    startOAuth({
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
    if (err instanceof NetworkError && err.failure === 'timeout') {
      dismissAuthSession();
      return null;
    }
    throw err;
  }
  return session.type === 'success' && session.url ? session.url : null;
}

async function exchangeRedirect(redirectUrl: string, router: AuthRouter): Promise<OAuthOutcome> {
  const outcome = await withAuthDeadline(
    completeAuthIntent(parseAuthLink(redirectUrl), router, authIntentClient()),
  );
  const exchanged = outcome.kind === 'success' || outcome.kind === 'deduped';
  if (exchanged) return { kind: 'ok' };
  const reason =
    outcome.kind === 'failure' && outcome.error ? failureReason(outcome.error) : 'unknown';
  return { kind: 'error', reason };
}

async function authorizeAndExchange(
  provider: OAuthProvider,
  router: AuthRouter,
): Promise<OAuthOutcome> {
  const authorization = await requestAuthorizationUrl(provider);
  if (authorization.kind !== 'authorization_url') return authorization;
  const redirectUrl = await redirectFromBrowser(authorization.url);
  if (!redirectUrl) return { kind: 'cancelled' };
  return exchangeRedirect(redirectUrl, router);
}

async function attemptSignIn(provider: OAuthProvider, router: AuthRouter): Promise<OAuthOutcome> {
  try {
    return await authorizeAndExchange(provider, router);
  } catch (err) {
    return thrownFailure(err);
  }
}

async function signInOutcome(
  authPort: AuthPort,
  provider: OAuthProvider,
  router: AuthRouter,
): Promise<OAuthOutcome> {
  const outcome = await attemptSignIn(provider, router);
  if (outcome.kind === 'error') authPort.reportSignInFailure(outcome.reason);
  return outcome;
}

export function useOAuth() {
  const navigator = useNavigator();
  const authPort = useAuthPort();
  return useOAuthFlow((provider) => signInOutcome(authPort, provider, navigator));
}
