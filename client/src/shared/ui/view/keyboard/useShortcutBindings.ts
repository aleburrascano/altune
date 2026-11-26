import { useEffect, useRef } from 'react';

import type { ShortcutTarget } from '@shared/ui/ports/shortcutTarget';
import { handleKeyboardEvent, type ShortcutPlayback } from '@shared/ui/core/keyboard/shortcuts';

type ListeningTarget = Required<ShortcutTarget>;

function resolveTarget(target: ShortcutTarget): ListeningTarget | null {
  const { addEventListener, removeEventListener } = target;
  if (typeof addEventListener !== 'function' || typeof removeEventListener !== 'function') {
    return null;
  }
  return {
    addEventListener: (type, listener) => addEventListener.call(target, type, listener),
    removeEventListener: (type, listener) => removeEventListener.call(target, type, listener),
  };
}

export function useShortcutBindings(
  playback: ShortcutPlayback,
  target: ShortcutTarget,
  focusSearch: () => void,
): void {
  const latestFocusSearch = useRef(focusSearch);

  useEffect(() => {
    latestFocusSearch.current = focusSearch;
  });

  useEffect(() => {
    const win = resolveTarget(target);
    if (!win) return undefined;
    const onKeyDown = (event: KeyboardEvent) =>
      handleKeyboardEvent(event, playback, () => latestFocusSearch.current());
    win.addEventListener('keydown', onKeyDown);
    return () => win.removeEventListener('keydown', onKeyDown);
  }, [target, playback]);
}
