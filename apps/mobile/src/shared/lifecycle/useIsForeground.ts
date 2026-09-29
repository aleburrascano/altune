import { useState } from 'react';

import { isAppActive } from './appState';
import { useAppStateChange } from './useAppStateChange';

export function useIsForeground(): boolean {
  const [isForeground, setIsForeground] = useState(isAppActive);

  useAppStateChange((state) => {
    setIsForeground(state === 'active');
  });

  return isForeground;
}
