import type { PropsWithChildren, ReactElement } from 'react';

import { PlayerNavigationProvider, type PlayerNavigation } from '@features/player';

type NavigationStep = 'player' | 'lyrics' | 'queue' | 'close';

export type RecordingPlayerNavigation = {
  navigation: PlayerNavigation;
  steps: NavigationStep[];
  wrapper: (props: PropsWithChildren) => ReactElement;
};

export function createRecordingPlayerNavigation(): RecordingPlayerNavigation {
  const steps: NavigationStep[] = [];
  const navigation: PlayerNavigation = {
    openPlayer: () => {
      steps.push('player');
    },
    openLyrics: () => {
      steps.push('lyrics');
    },
    openQueue: () => {
      steps.push('queue');
    },
    close: () => {
      steps.push('close');
    },
  };
  function wrapper({ children }: PropsWithChildren): ReactElement {
    return <PlayerNavigationProvider navigation={navigation}>{children}</PlayerNavigationProvider>;
  }
  return { navigation, steps, wrapper };
}
