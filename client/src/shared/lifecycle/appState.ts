import { AppState } from 'react-native';

export type AppLifecycleState = 'active' | 'background' | 'inactive' | 'unknown' | 'extension';

export function subscribeAppState(listener: (state: AppLifecycleState) => void): () => void {
  const subscription = AppState.addEventListener('change', listener);
  return () => subscription.remove();
}

export function isAppActive(): boolean {
  return AppState.currentState === 'active';
}
