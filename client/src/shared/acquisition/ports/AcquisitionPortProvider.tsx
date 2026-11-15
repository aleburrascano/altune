import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';

const AcquisitionPortContext = createContext<AcquisitionPort | null>(null);

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
  const port = use(AcquisitionPortContext);
  if (port === null) throw new Error('no acquisition port provided: mount AcquisitionPortProvider');
  return port;
}
