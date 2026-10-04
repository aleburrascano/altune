import { createContext, useContext } from 'react';
import type { ReactElement, ReactNode } from 'react';

import { httpLibraryPort } from '@features/library/adapters/http/httpLibraryPort';
import type { LibraryPort } from '@features/library/ports/libraryPort';

const LibraryPortContext = createContext<LibraryPort>(httpLibraryPort);

interface LibraryPortProviderProps {
  port: LibraryPort;
  children: ReactNode;
}

export function LibraryPortProvider({ port, children }: LibraryPortProviderProps): ReactElement {
  return <LibraryPortContext.Provider value={port}>{children}</LibraryPortContext.Provider>;
}

export function useLibraryPort(): LibraryPort {
  return useContext(LibraryPortContext);
}
