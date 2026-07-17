/**
 * buildTrackMenuItems — the one place the track context menu is assembled
 * (structure audit F2: three screens each built their own near-identical copy
 * and had already drifted). The invariant part: queue actions gated on the
 * track being ready, then View Details, then a danger row. Callers pass the
 * bits that genuinely differ per screen: the optional Add to Playlist entry,
 * the details navigation, and the danger action.
 */
import type { TrackResponse } from '@shared/api-client/types';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack } from '@shared/playback/types';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';

type QueueActions = {
  playNext: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
};

export function buildTrackMenuItems(
  track: TrackResponse,
  opts: {
    queue: QueueActions;
    onViewDetails: () => void;
    /** Present only where the screen offers it (the Library tracks list). */
    onAddToPlaylist?: () => void;
    danger: { label: string; onPress: () => void };
  },
): ContextMenuItem[] {
  const ready = track.acquisition_status === 'ready';
  return [
    ...(ready
      ? [
          { label: 'Play Next', onPress: () => opts.queue.playNext(toPlaybackTrack(track)) },
          { label: 'Add to Queue', onPress: () => opts.queue.addToQueue(toPlaybackTrack(track)) },
        ]
      : []),
    ...(opts.onAddToPlaylist ? [{ label: 'Add to Playlist', onPress: opts.onAddToPlaylist }] : []),
    { label: 'View Details', onPress: opts.onViewDetails },
    { label: opts.danger.label, tone: 'danger' as const, onPress: opts.danger.onPress },
  ];
}
