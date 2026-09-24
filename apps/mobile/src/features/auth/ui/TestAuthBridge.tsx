import { useEffect, useRef } from 'react';

import { bootstrapTestAuth, isTestAuthEnabled } from '../testAuth';

/**
 * Mounts the NON-PRODUCTION test-auth bootstrap (see
 * docs/features/webauth-testing/design.md). When enabled — a dev build with
 * `EXPO_PUBLIC_TEST_AUTH=1` — it logs in the dedicated test user via go-api's
 * `/test/login` and injects the session so authed screens render for an
 * automated driver. It renders nothing and is inert in every other build: the
 * guard is false and Metro strips the path from a production bundle.
 *
 * Must be mounted OUTSIDE the AuthGate so it still runs while signed-out (the
 * gate redirects away from its children before they can mount).
 */
export function TestAuthBridge(): null {
  const started = useRef(false);

  useEffect(() => {
    if (started.current || !isTestAuthEnabled()) {
      return;
    }
    started.current = true;
    void bootstrapTestAuth().catch((error: unknown) => {
      console.warn('[test-auth] bootstrap failed', error);
    });
  }, []);

  return null;
}
