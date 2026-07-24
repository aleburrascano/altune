/**
 * buildTrackMenuItems — the one place the track context menu is assembled
 * (structure audit F2: three screens each built their own near-identical copy
 * and had already drifted). The invariant part: queue actions gated on the
 * track being ready, then View Details, then a danger row. Callers pass the
 * bits that genuinely differ per screen: the optional Add to Playlist entry,
 * the details navigation, and the danger action.
 */
import { usePinnedStore } from '@shared/offline/pinnedStore';
import type { TrackResponse } from '@shared/api-client/types';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack } from '@shared/playback/types';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';

type QueueActions = {
  playNext: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
};

/** The offline row, read from the pinned store at build time. The menu is rebuilt
 *  every open, so the label always reflects the track's current state. */
function offlineItem(trackId: string): ContextMenuItem {
  const { entries, pin, unpin } = usePinnedStore.getState();
  const status = entries[trackId]?.status;
  if (status === 'ready') {
    return { label: 'Remove download', onPress: () => unpin(trackId) };
  }
  if (status === 'queued' || status === 'downloading') {
    return { label: 'Cancel download', onPress: () => unpin(trackId) };
  }
  return {
    label: status === 'failed' ? 'Retry download' : 'Download',
    onPress: () => pin(trackId),
  };
}

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
    // Offline is only meaningful once the server actually has the audio, so it
    // sits behind the same `ready` gate as the queue actions.
    ...(ready ? [offlineItem(track.id)] : []),
    { label: 'View Details', onPress: opts.onViewDetails },
    { label: opts.danger.label, tone: 'danger' as const, onPress: opts.danger.onPress },
  ];
}
