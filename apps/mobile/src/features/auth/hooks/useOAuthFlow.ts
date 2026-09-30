import { useEffect, useRef, useState } from 'react';

import type { OAuthOutcome, OAuthProvider, OAuthResult } from '../oauthResult';

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
