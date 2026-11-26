import { useRef, useState } from 'react';

import type { OAuthOutcome, OAuthProvider, OAuthResult } from '@features/signIn/core/oauthResult';

type Run = (provider: OAuthProvider) => Promise<OAuthOutcome | null>;

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
  setState: (state: OAuthResult) => void,
): Promise<void> {
  setState({ kind: 'pending', provider });
  const outcome = await run(provider);
  if (outcome) setState(outcome);
}

export function useOAuthFlow(run: Run) {
  const [state, setState] = useState<OAuthResult>({ kind: 'idle' });
  const signInWith = useSingleFlight((provider: OAuthProvider) => settle(provider, run, setState));
  return { state, signInWith };
}
