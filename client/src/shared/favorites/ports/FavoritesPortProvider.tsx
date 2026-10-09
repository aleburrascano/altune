import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import { httpFavoritesPort } from '@shared/favorites/adapters/http/httpFavoritesPort';
import type { FavoritesPort } from '@shared/favorites/ports/favoritesPort';

const FavoritesPortContext = createContext<FavoritesPort>(httpFavoritesPort);

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
  return use(FavoritesPortContext);
}
