import { useEffect, useRef } from 'react';
import { AppState, type AppStateStatus } from 'react-native';

export function useAppStateChange(onChange: (state: AppStateStatus) => void): void {
  const handlerRef = useRef(onChange);

  useEffect(() => {
    handlerRef.current = onChange;
  }, [onChange]);

  useEffect(() => {
    const sub = AppState.addEventListener('change', (state) => handlerRef.current(state));
    return () => sub.remove();
  }, []);
}
