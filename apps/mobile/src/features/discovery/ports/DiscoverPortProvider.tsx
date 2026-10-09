import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import { httpDiscoverPort } from '@features/discovery/adapters/http/httpDiscoverPort';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

const DiscoverPortContext = createContext<DiscoverPort>(httpDiscoverPort);

interface DiscoverPortProviderProps {
  port: DiscoverPort;
  children: ReactNode;
}

export function DiscoverPortProvider({ port, children }: DiscoverPortProviderProps): ReactElement {
  return <DiscoverPortContext.Provider value={port}>{children}</DiscoverPortContext.Provider>;
}

export function useDiscoverPort(): DiscoverPort {
  return use(DiscoverPortContext);
}
