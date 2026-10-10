import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { FavoritesPort } from '@features/discovery/ports/favoritesPort';

const FavoritesPortContext = createContext<FavoritesPort | null>(null);

interface FavoritesPortProviderProps {
  port: FavoritesPort;
  children: ReactNode;
}

export function FavoritesPortProvider({
  port,
  children,
}: FavoritesPortProviderProps): ReactElement {
  return <FavoritesPortContext.Provider value={port}>{children}</FavoritesPortContext.Provider>;
}

export function useFavoritesPort(): FavoritesPort {
  const port = use(FavoritesPortContext);
  if (port === null) throw new Error('no favorites port provided: mount FavoritesPortProvider');
  return port;
}
