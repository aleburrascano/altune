import { useMemo, type ReactElement, type ReactNode } from 'react';

import { useNavigator, type Navigator } from '@shared/navigation';
import { PlayerNavigationProvider, type PlayerNavigation } from '@features/player';

function playerNavigationOver(router: Navigator): PlayerNavigation {
  return {
    openPlayer: () => {
      router.push('/player');
    },
    openLyrics: () => {
      router.push('/player/lyrics');
    },
    openQueue: () => {
      router.push('/player/queue');
    },
    close: () => {
      router.back();
    },
  };
}

interface PlayerNavigationBridgeProps {
  children: ReactNode;
}

export function PlayerNavigationBridge({ children }: PlayerNavigationBridgeProps): ReactElement {
  const router = useNavigator();
  const navigation = useMemo(() => playerNavigationOver(router), [router]);
  return <PlayerNavigationProvider navigation={navigation}>{children}</PlayerNavigationProvider>;
}
