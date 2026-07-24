import { useEffect, useState } from 'react';
import { AppState, type AppStateStatus } from 'react-native';

export function useIsForeground(): boolean {
  const [isForeground, setIsForeground] = useState(() => AppState.currentState === 'active');

  useEffect(() => {
    const onChange = (state: AppStateStatus): void => {
      setIsForeground(state === 'active');
    };
    const sub = AppState.addEventListener('change', onChange);
    return () => sub.remove();
  }, []);

  return isForeground;
}
