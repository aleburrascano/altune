import { NetworkError } from '@shared/failure';
import { startOAuth, withAuthDeadline } from '@shared/auth';
import { useNavigator } from '@shared/navigation';
import { authRedirectUrl } from '@features/signIn/core/authRedirect';
import type { AuthBrowser, AuthIntents, AuthPort, AuthRouter } from '@features/signIn/ports/ports';
import {
  failureReason,
  OAUTH_BROWSER_TIMEOUT_MS,
  thrownFailure,
  type OAuthFailure,
  type OAuthOutcome,
  type OAuthProvider,
} from '@features/signIn/core/oauthResult';
import { parseAuthLink } from '@features/signIn/core/parseAuthLink';
import { useAuthBrowser, useAuthIntents, useAuthPort } from '@features/signIn/view/SignInProvider';

import { useOAuthFlow } from '@features/signIn/view/useOAuthFlow';
import { isPresent } from '@features/signIn/core/isPresent';

type AuthorizationRequest = { kind: 'authorization_url'; url: string } | OAuthFailure;

async function requestAuthorizationUrl(provider: OAuthProvider): Promise<AuthorizationRequest> {
  const { data, error } = await withAuthDeadline(
    startOAuth({
      provider,
      options: { redirectTo: authRedirectUrl('callback'), skipBrowserRedirect: true },
    }),
  );
  if (error) return { kind: 'error', reason: failureReason(error) };
  if (!isPresent(data.url)) return { kind: 'error', reason: 'unknown' };
  return { kind: 'authorization_url', url: data.url };
}

async function redirectFromBrowser(
  browser: AuthBrowser,
  authorizationUrl: string,
): Promise<string | null> {
  let session: Awaited<ReturnType<AuthBrowser['openAuthSession']>>;
  try {
    session = await withAuthDeadline(
      browser.openAuthSession(authorizationUrl, authRedirectUrl('callback')),
      OAUTH_BROWSER_TIMEOUT_MS,
    );
  } catch (err) {
    if (err instanceof NetworkError && err.failure === 'timeout') {
      browser.dismissAuthSession();
      return null;
    }
    throw err;
  }
  return session.type === 'success' && isPresent(session.url) ? session.url : null;
}

async function exchangeRedirect(
  intents: AuthIntents,
  redirectUrl: string,
  router: AuthRouter,
): Promise<OAuthOutcome> {
  const outcome = await withAuthDeadline(intents.complete(parseAuthLink(redirectUrl), router));
  const exchanged = outcome.kind === 'success' || outcome.kind === 'deduped';
  if (exchanged) return { kind: 'ok' };
  const reason =
    outcome.kind === 'failure' && outcome.error ? failureReason(outcome.error) : 'unknown';
  return { kind: 'error', reason };
}

async function authorizeAndExchange(
  browser: AuthBrowser,
  intents: AuthIntents,
  provider: OAuthProvider,
  router: AuthRouter,
): Promise<OAuthOutcome> {
  const authorization = await requestAuthorizationUrl(provider);
  if (authorization.kind !== 'authorization_url') return authorization;
  const redirectUrl = await redirectFromBrowser(browser, authorization.url);
  if (!isPresent(redirectUrl)) return { kind: 'cancelled' };
  return exchangeRedirect(intents, redirectUrl, router);
}

async function attemptSignIn(
  browser: AuthBrowser,
  intents: AuthIntents,
  provider: OAuthProvider,
  router: AuthRouter,
): Promise<OAuthOutcome> {
  try {
    return await authorizeAndExchange(browser, intents, provider, router);
  } catch (err) {
    return thrownFailure(err);
  }
}

async function signInOutcome(
  authPort: AuthPort,
  browser: AuthBrowser,
  intents: AuthIntents,
  provider: OAuthProvider,
  router: AuthRouter,
): Promise<OAuthOutcome> {
  const outcome = await attemptSignIn(browser, intents, provider, router);
  if (outcome.kind === 'error') authPort.reportSignInFailure(outcome.reason);
  return outcome;
}

export function useOAuth() {
  const navigator = useNavigator();
  const authPort = useAuthPort();
  const browser = useAuthBrowser();
  const intents = useAuthIntents();
  return useOAuthFlow((provider) => signInOutcome(authPort, browser, intents, provider, navigator));
}
