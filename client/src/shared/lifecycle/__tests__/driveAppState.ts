import { setAppStateSource, type AppLifecycleState } from '@shared/lifecycle';

type AppStateListener = (state: AppLifecycleState) => void;

export interface AppStateDriver {
  change(next: AppLifecycleState): void;
}

export function driveAppState(initial: AppLifecycleState = 'active'): AppStateDriver {
  let state = initial;
  const listeners = new Set<AppStateListener>();
  setAppStateSource({
    current: () => state,
    onChange: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  });
  return {
    change(next) {
      state = next;
      for (const listener of [...listeners]) listener(next);
    },
  };
}
