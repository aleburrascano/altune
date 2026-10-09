import { useState } from 'react';

import { isAppActive } from '@shared/lifecycle/appState';
import { useAppStateChange } from '@shared/lifecycle/useAppStateChange';

export function useIsForeground(): boolean {
  const [isForeground, setIsForeground] = useState(isAppActive);

  useAppStateChange((state) => {
    setIsForeground(state === 'active');
  });

  return isForeground;
}
