import { createContext, use, useMemo } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { SignInHealth } from '@features/signIn/core/signInHealth';
import type { RecoveryUnlockSource } from '@features/signIn/core/recoveryUnlockSource';
import type {
  AttemptLockout,
  AuthBrowser,
  AuthIntents,
  AuthPort,
} from '@features/signIn/ports/ports';

type AuthPorts = {
  port: AuthPort;
  browser: AuthBrowser;
  lockout: AttemptLockout;
  recovery: RecoveryUnlockSource;
  intents: AuthIntents;
  health: SignInHealth;
};

const AuthPortContext = createContext<AuthPorts | null>(null);

interface SignInProviderProps extends AuthPorts {
  children: ReactNode;
}

export function SignInProvider({
  port,
  browser,
  lockout,
  recovery,
  intents,
  health,
  children,
}: SignInProviderProps): ReactElement {
  const ports = useMemo(
    () => ({ port, browser, lockout, recovery, intents, health }),
    [port, browser, lockout, recovery, intents, health],
  );
  return <AuthPortContext.Provider value={ports}>{children}</AuthPortContext.Provider>;
}

function useAuthPorts(): AuthPorts {
  const ports = use(AuthPortContext);
  if (ports === null) throw new Error('no auth port provided: mount SignInProvider');
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

export function useSignInHealth(): SignInHealth {
  return useAuthPorts().health;
}

export function useAuthIntents(): AuthIntents {
  return useAuthPorts().intents;
}
