import { useState } from 'react';
import { AppState } from 'react-native';

import { useAppStateChange } from './useAppStateChange';

export function useIsForeground(): boolean {
  const [isForeground, setIsForeground] = useState(() => AppState.currentState === 'active');

  useAppStateChange((state) => {
    setIsForeground(state === 'active');
  });

  return isForeground;
}
