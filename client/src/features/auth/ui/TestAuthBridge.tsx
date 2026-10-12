import { useEffect, useRef } from 'react';

import { useAuthPort } from '@features/auth/ports/AuthPortProvider';

export function TestAuthBridge(): null {
  const authPort = useAuthPort();
  const started = useRef(false);

  useEffect(() => {
    if (started.current || !authPort.isTestAuthEnabled()) {
      return;
    }
    started.current = true;
    void authPort.bootstrapTestAuth().catch((error: unknown) => {
      console.warn('[test-auth] bootstrap failed', error);
    });
  }, [authPort]);

  return null;
}
