import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { DetailPort } from '@features/detail/ports/detailPort';

const DetailPortContext = createContext<DetailPort | null>(null);

interface DetailPortProviderProps {
  port: DetailPort;
  children: ReactNode;
}

export function DetailPortProvider({ port, children }: DetailPortProviderProps): ReactElement {
  return <DetailPortContext.Provider value={port}>{children}</DetailPortContext.Provider>;
}

export function useDetailPort(): DetailPort {
  const port = use(DetailPortContext);
  if (port === null) throw new Error('no detail port provided: mount DetailPortProvider');
  return port;
}
