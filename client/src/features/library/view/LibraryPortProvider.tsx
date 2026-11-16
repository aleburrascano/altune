import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { LibraryPort } from '@features/library/ports/libraryPort';

const LibraryPortContext = createContext<LibraryPort | null>(null);

interface LibraryPortProviderProps {
  port: LibraryPort;
  children: ReactNode;
}

export function LibraryPortProvider({ port, children }: LibraryPortProviderProps): ReactElement {
  return <LibraryPortContext.Provider value={port}>{children}</LibraryPortContext.Provider>;
}

export function useLibraryPort(): LibraryPort {
  const port = use(LibraryPortContext);
  if (port === null) throw new Error('no library port provided: mount LibraryPortProvider');
  return port;
}
