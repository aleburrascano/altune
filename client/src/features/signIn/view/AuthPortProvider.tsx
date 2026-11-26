import { createContext, use, useMemo } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { RecoveryUnlockSource } from '@features/signIn/core/recoveryUnlockSource';
import type { AttemptLockout } from '@features/signIn/ports/attemptLockout';
import type { AuthBrowser } from '@features/signIn/ports/authBrowser';
import type { AuthIntents } from '@features/signIn/ports/authIntents';
import type { AuthPort } from '@features/signIn/ports/authPort';

type AuthPorts = {
  port: AuthPort;
  browser: AuthBrowser;
  lockout: AttemptLockout;
  recovery: RecoveryUnlockSource;
  intents: AuthIntents;
};

const AuthPortContext = createContext<AuthPorts | null>(null);

interface AuthPortProviderProps extends AuthPorts {
  children: ReactNode;
}

export function AuthPortProvider({
  port,
  browser,
  lockout,
  recovery,
  intents,
  children,
}: AuthPortProviderProps): ReactElement {
  const ports = useMemo(
    () => ({ port, browser, lockout, recovery, intents }),
    [port, browser, lockout, recovery, intents],
  );
  return <AuthPortContext.Provider value={ports}>{children}</AuthPortContext.Provider>;
}

function useAuthPorts(): AuthPorts {
  const ports = use(AuthPortContext);
  if (ports === null) throw new Error('no auth port provided: mount AuthPortProvider');
  return ports;
}

export function useAuthPort(): AuthPort {
  return useAuthPorts().port;
}

export function useAuthBrowser(): AuthBrowser {
  return useAuthPorts().browser;
}

export function useAttemptLockout(): AttemptLockout {
  return useAuthPorts().lockout;
}

export function useRecoveryUnlock(): RecoveryUnlockSource {
  return useAuthPorts().recovery;
}

export function useAuthIntents(): AuthIntents {
  return useAuthPorts().intents;
}
