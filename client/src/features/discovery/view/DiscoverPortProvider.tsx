import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

const DiscoverPortContext = createContext<DiscoverPort | null>(null);

interface DiscoverPortProviderProps {
  port: DiscoverPort;
  children: ReactNode;
}

export function DiscoverPortProvider({ port, children }: DiscoverPortProviderProps): ReactElement {
  return <DiscoverPortContext.Provider value={port}>{children}</DiscoverPortContext.Provider>;
}

export function useDiscoverPort(): DiscoverPort {
  const port = use(DiscoverPortContext);
  if (port === null) throw new Error('no discover port provided: mount DiscoverPortProvider');
  return port;
}
