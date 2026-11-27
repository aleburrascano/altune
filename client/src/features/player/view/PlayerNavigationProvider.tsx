import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { PlayerNavigation } from '@features/player/ports/playerNavigation';

const PlayerNavigationContext = createContext<PlayerNavigation | null>(null);

interface PlayerNavigationProviderProps {
  navigation: PlayerNavigation;
  children: ReactNode;
}

export function PlayerNavigationProvider({
  navigation,
  children,
}: PlayerNavigationProviderProps): ReactElement {
  return (
    <PlayerNavigationContext.Provider value={navigation}>
      {children}
    </PlayerNavigationContext.Provider>
  );
}

export function usePlayerNavigation(): PlayerNavigation {
  const navigation = use(PlayerNavigationContext);
  if (navigation === null) {
    throw new Error('no player navigation provided: mount PlayerNavigationProvider');
  }
  return navigation;
}
