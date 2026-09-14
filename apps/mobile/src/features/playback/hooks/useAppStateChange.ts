import { useEffect } from 'react';
import { AppState, type AppStateStatus } from 'react-native';

/**
 * Subscribes to AppState `change` events for the component's lifetime and
 * removes the listener on unmount. Re-subscribes when `onChange` changes, so
 * callers should memoize it.
 */
export function useAppStateChange(onChange: (state: AppStateStatus) => void): void {
  useEffect(() => {
    const sub = AppState.addEventListener('change', onChange);
    return () => sub.remove();
  }, [onChange]);
}
