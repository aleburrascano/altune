import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { LibraryPort } from '@features/library/ports/ports';

const LibraryPortContext = createContext<LibraryPort | null>(null);

interface LibraryPortProviderProps {
  port: LibraryPort;
  children: ReactNode;
}

export function LibraryProvider({ port, children }: LibraryPortProviderProps): ReactElement {
  return <LibraryPortContext.Provider value={port}>{children}</LibraryPortContext.Provider>;
}

export function useLibraryPort(): LibraryPort {
  const port = use(LibraryPortContext);
  if (port === null) throw new Error('no library port provided: mount LibraryProvider');
  return port;
}
