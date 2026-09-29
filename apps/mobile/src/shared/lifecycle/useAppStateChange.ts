import { useEffect, useRef } from 'react';

import { type AppLifecycleState, subscribeAppState } from './appState';

export function useAppStateChange(listener: (state: AppLifecycleState) => void): void {
  const handlerRef = useRef(listener);

  useEffect(() => {
    handlerRef.current = listener;
  }, [listener]);

  useEffect(() => subscribeAppState((state) => handlerRef.current(state)), []);
}
