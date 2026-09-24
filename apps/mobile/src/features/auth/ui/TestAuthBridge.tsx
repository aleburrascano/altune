import { useEffect, useRef } from 'react';

import { bootstrapTestAuth, isTestAuthEnabled } from '../testAuth';

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
