import { useCallback, useState } from 'react';
import { AppState, type AppStateStatus } from 'react-native';

import { useAppStateChange } from './useAppStateChange';

export function useIsForeground(): boolean {
  const [isForeground, setIsForeground] = useState(() => AppState.currentState === 'active');

  const onChange = useCallback((state: AppStateStatus): void => {
    setIsForeground(state === 'active');
  }, []);
  useAppStateChange(onChange);

  return isForeground;
}
