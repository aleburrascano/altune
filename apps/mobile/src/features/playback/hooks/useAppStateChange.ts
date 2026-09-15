import { useEffect, useRef } from 'react';
import { AppState, type AppStateStatus } from 'react-native';

/**
 * Subscribes to AppState `change` events for the component's lifetime and
 * removes the listener on unmount. The subscription is made once; each event
 * runs the latest `onChange`, so callers need not memoize it.
 */
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
