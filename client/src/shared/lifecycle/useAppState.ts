import { useEffect, useRef, useState } from 'react';

import { type AppLifecycleState, isAppActive, subscribeAppState } from '@shared/lifecycle/appState';

export function useAppStateChange(listener: (state: AppLifecycleState) => void): void {
  const handlerRef = useRef(listener);

  useEffect(() => {
    handlerRef.current = listener;
  }, [listener]);

  useEffect(() => subscribeAppState((state) => handlerRef.current(state)), []);
}

export function useIsForeground(): boolean {
  const [isForeground, setIsForeground] = useState(isAppActive);

  useAppStateChange((state) => {
    setIsForeground(state === 'active');
  });

  return isForeground;
}
