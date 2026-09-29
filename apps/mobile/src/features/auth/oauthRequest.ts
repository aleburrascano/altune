import { useEffect, useRef, useState } from 'react';

import { isNetworkError } from '@shared/lib/isNetworkError';

import type { AuthErrorReason } from './errorReason';
import {
  isRateLimitedAuthError,
  isTransportAuthError,
  type SupabaseAuthErrorLike,
} from './supabaseAuthError';

export type OAuthProvider = 'google';

export type OAuthResult =
  | { kind: 'idle' }
  | { kind: 'pending'; provider: OAuthProvider }
  | { kind: 'ok' }
  | { kind: 'cancelled' }
  | {
      kind: 'error';
      reason: Extract<AuthErrorReason, 'network' | 'unknown' | 'too_many_attempts'>;
    };

export type OAuthOutcome = Exclude<OAuthResult, { kind: 'idle' } | { kind: 'pending' }>;
export type OAuthFailure = Extract<OAuthOutcome, { kind: 'error' }>;

export const OAUTH_BROWSER_TIMEOUT_MS = 5 * 60_000;

export function failureReason(error: SupabaseAuthErrorLike): OAuthFailure['reason'] {
  if (isRateLimitedAuthError(error)) return 'too_many_attempts';
  return isTransportAuthError(error) ? 'network' : 'unknown';
}

export function thrownFailure(err: unknown): OAuthFailure {
  return { kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' };
}

type Run = (provider: OAuthProvider) => Promise<OAuthOutcome | null>;

function useMountedRef() {
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return mounted;
}

async function runOnce(busy: { current: boolean }, task: () => Promise<void>): Promise<void> {
  if (busy.current) return;
  busy.current = true;
  try {
    await task();
  } finally {
    busy.current = false;
  }
}

function useSingleFlight<A>(fn: (arg: A) => Promise<void>) {
  const busy = useRef(false);
  return (arg: A) => runOnce(busy, () => fn(arg));
}

async function settle(
  provider: OAuthProvider,
  run: Run,
  mounted: { current: boolean },
  setState: (state: OAuthResult) => void,
): Promise<void> {
  setState({ kind: 'pending', provider });
  const outcome = await run(provider);
  if (outcome && mounted.current) setState(outcome);
}

export function useOAuthFlow(run: Run) {
  const [state, setState] = useState<OAuthResult>({ kind: 'idle' });
  const mounted = useMountedRef();
  const signInWith = useSingleFlight((provider: OAuthProvider) =>
    settle(provider, run, mounted, setState),
  );
  return { state, signInWith };
}
