import { createContext, useContext } from 'react';
import type { ReactElement, ReactNode } from 'react';

import { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
import type { DetailPort } from '@features/detail/ports/detailPort';

const DetailPortContext = createContext<DetailPort>(httpDetailPort);

interface DetailPortProviderProps {
  port: DetailPort;
  children: ReactNode;
}

export function DetailPortProvider({ port, children }: DetailPortProviderProps): ReactElement {
  return <DetailPortContext.Provider value={port}>{children}</DetailPortContext.Provider>;
}

export function useDetailPort(): DetailPort {
  return useContext(DetailPortContext);
}
