import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { AuthPort } from '@features/auth/ports/authPort';

const AuthPortContext = createContext<AuthPort | null>(null);

interface AuthPortProviderProps {
  port: AuthPort;
  children: ReactNode;
}

export function AuthPortProvider({ port, children }: AuthPortProviderProps): ReactElement {
  return <AuthPortContext.Provider value={port}>{children}</AuthPortContext.Provider>;
}

export function useAuthPort(): AuthPort {
  const port = use(AuthPortContext);
  if (port === null) throw new Error('no auth port provided: mount AuthPortProvider');
  return port;
}
