import { useRouter } from 'expo-router';
import * as WebBrowser from 'expo-web-browser';
import { useEffect, useRef, useState } from 'react';
import { Platform } from 'react-native';

import { NetworkError } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';
import { isNetworkError } from '@shared/lib/isNetworkError';

import { withAuthDeadline } from '../authDeadline';
import { completeAuthIntent, type AuthRouter } from '../completeAuthIntent';
import type { AuthErrorReason } from '../errorReason';
import { authRedirectUrl, parseAuthLink } from '../parseAuthLink';
import {
  isRateLimitedAuthError,
  isTransportAuthError,
  type SupabaseAuthErrorLike,
} from '../supabaseAuthError';

WebBrowser.maybeCompleteAuthSession();

export type OAuthProvider = 'google';

export type OAuthResult =
  | { kind: 'idle' }
  | { kind: 'pending'; provider: OAuthProvider }
  | { kind: 'ok' }
  | { kind: 'cancelled' }
  | { kind: 'error'; reason: Extract<AuthErrorReason, 'network' | 'unknown' | 'too_many_attempts'> };

type OAuthOutcome = Exclude<OAuthResult, { kind: 'idle' } | { kind: 'pending' }>;
type OAuthFailure = Extract<OAuthOutcome, { kind: 'error' }>;

/**
 * The browser leg is paced by a human typing at the provider, so the 20 s SDK
 * budget would abandon sign-ins that are going fine. Past this one, the session
 * is gone (the app was backgrounded and never came back) and the button must
 * not stay pinned at `pending`; a redirect arriving later is still claimed by
 * the global deep-link listener.
 */
export const OAUTH_BROWSER_TIMEOUT_MS = 5 * 60_000;

type AuthorizationRequest = { kind: 'authorization_url'; url: string } | OAuthFailure;

function failureReason(error: SupabaseAuthErrorLike): OAuthFailure['reason'] {
  if (isRateLimitedAuthError(error)) return 'too_many_attempts';
  return isTransportAuthError(error) ? 'network' : 'unknown';
}

/** The provider's hosted sign-in URL, or the failure that stands in for it. */
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

function thrownFailure(err: unknown): OAuthFailure {
  return { kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' };
}

async function beginWebRedirect(provider: OAuthProvider): Promise<OAuthFailure | null> {
  try {
    const { error } = await withAuthDeadline(
      supabase.auth.signInWithOAuth({ provider, options: { redirectTo: authRedirectUrl('callback') } }),
    );
    return error ? { kind: 'error', reason: failureReason(error) } : null;
  } catch (err) {
    return thrownFailure(err);
  }
}

/** The callback URL the in-app browser came back with, or null if it was dismissed. */
async function redirectFromBrowser(authorizationUrl: string): Promise<string | null> {
  let session: WebBrowser.WebBrowserAuthSessionResult;
  try {
    session = await withAuthDeadline(
      WebBrowser.openAuthSessionAsync(authorizationUrl, authRedirectUrl('callback')),
      OAUTH_BROWSER_TIMEOUT_MS,
    );
  } catch (err) {
    if (err instanceof NetworkError && err.failure === 'timeout') return null;
    throw err;
  }
  return session.type === 'success' && session.url ? session.url : null;
}

/**
 * `ok` only if the code exchange actually succeeded. `deduped` is the global
 * deep-link listener's *confirmed* success on this same callback, so it is one
 * too; when that listener's exchange failed, this delivery is told the failure
 * rather than `deduped` (#1641). Anything else — a rejected exchange or an
 * unrecognized callback — is a real error.
 */
async function exchangeRedirect(redirectUrl: string, router: AuthRouter): Promise<OAuthOutcome> {
  const outcome = await withAuthDeadline(
    completeAuthIntent(parseAuthLink(redirectUrl), router, supabase.auth),
  );
  const exchanged = outcome.kind === 'success' || outcome.kind === 'deduped';
  if (exchanged) return { kind: 'ok' };
  const reason = outcome.kind === 'failure' && outcome.error ? failureReason(outcome.error) : 'unknown';
  return { kind: 'error', reason };
}

/** Every leg of the flow, reported as one terminal state and never thrown. */
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
  const router = useRouter();
  const [state, setState] = useState<OAuthResult>({ kind: 'idle' });
  const mounted = useRef(true);
  const inFlight = useRef(false);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // A ref, not `state`: two presses in the same tick both read a `state` React
  // has not re-rendered yet, so only a synchronous flag keeps the second one
  // from opening a second browser session over the first.
  async function signInWith(provider: OAuthProvider): Promise<void> {
    if (inFlight.current) return;
    inFlight.current = true;
    try {
      setState({ kind: 'pending', provider });
      if (Platform.OS === 'web') {
        const failure = await beginWebRedirect(provider);
        if (failure && mounted.current) setState(failure);
        return;
      }
      const outcome = await signInOutcome(provider, router);
      // The browser session routinely outlives the screen that opened it: a user
      // who navigated away has no banner left to show this to.
      if (!mounted.current) return;
      setState(outcome);
    } finally {
      inFlight.current = false;
    }
  }

  return { state, signInWith };
}
