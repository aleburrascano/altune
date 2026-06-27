import { useEffect, useState } from 'react';
import { AppState, type AppStateStatus } from 'react-native';

// AIDEV-NOTE: True only while the app is frontmost. Used to gate the player's
// JS-thread position UI updates: a backgrounded, screen-off app has no reason to
// re-render the scrubber/mini-player twice a second, and doing so burns CPU that
// trips iOS's background CPU watchdog (Process killed, bug_type 206) during
// long listening sessions. Native audio playback is unaffected — it runs on the
// native side regardless of this flag.
export function useIsForeground(): boolean {
  const [isForeground, setIsForeground] = useState(
    () => AppState.currentState === 'active',
  );

  useEffect(() => {
    const onChange = (state: AppStateStatus): void => {
      setIsForeground(state === 'active');
    };
    const sub = AppState.addEventListener('change', onChange);
    return () => sub.remove();
  }, []);

  return isForeground;
}
