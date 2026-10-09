import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import { httpAcquisitionPort } from '@shared/acquisition/adapters/http/httpAcquisitionPort';
import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';

const AcquisitionPortContext = createContext<AcquisitionPort>(httpAcquisitionPort);

interface AcquisitionPortProviderProps {
  port: AcquisitionPort;
  children: ReactNode;
}

export function AcquisitionPortProvider({
  port,
  children,
}: AcquisitionPortProviderProps): ReactElement {
  return <AcquisitionPortContext.Provider value={port}>{children}</AcquisitionPortContext.Provider>;
}

export function useAcquisitionPort(): AcquisitionPort {
  return use(AcquisitionPortContext);
}
