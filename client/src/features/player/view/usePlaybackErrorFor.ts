import { useMemo } from 'react';

import type { TrackKey } from '@shared/playback';

import { usePlaybackErrorStore } from '@features/player/core/playbackErrorStore';
import type { RedactedPlaybackFailure } from '@features/player/core/playbackError';

export function usePlaybackErrorFor(key: TrackKey | null): RedactedPlaybackFailure | null {
  const kind = usePlaybackErrorStore((s) => (key != null && s.key === key ? s.kind : null));
  const message = usePlaybackErrorStore((s) => (key != null && s.key === key ? s.message : null));

  return useMemo(
    () => (kind !== null && message !== null ? { kind, message } : null),
    [kind, message],
  );
}
