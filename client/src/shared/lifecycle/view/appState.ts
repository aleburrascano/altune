import { AppState } from 'react-native';

export type AppLifecycleState = 'active' | 'background' | 'inactive' | 'unknown' | 'extension';

type AppStateListener = (state: AppLifecycleState) => void;

export interface AppStateSource {
  current(): AppLifecycleState;
  onChange(listener: AppStateListener): () => void;
}

const nativeAppState: AppStateSource = {
  current: () => AppState.currentState,
  onChange: (listener) => {
    const subscription = AppState.addEventListener('change', listener);
    return () => subscription.remove();
  },
};

const subscriptions = new Map<AppStateListener, () => void>();
let source = nativeAppState;

export function setAppStateSource(next: AppStateSource = nativeAppState): void {
  if (next === source) return;
  source = next;
  for (const [listener, detach] of subscriptions) {
    detach();
    subscriptions.set(listener, source.onChange(listener));
  }
}

export function subscribeAppState(listener: AppStateListener): () => void {
  function subscriber(state: AppLifecycleState): void {
    listener(state);
  }
  subscriptions.set(subscriber, source.onChange(subscriber));
  return () => {
    subscriptions.get(subscriber)?.();
    subscriptions.delete(subscriber);
  };
}

export function isAppActive(): boolean {
  return source.current() === 'active';
}
