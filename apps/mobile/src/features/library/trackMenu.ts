import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack } from '@shared/playback/types';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';

import { reportStorageFull } from './pinBatchSummary';
import type { LibraryOffline } from './hooks/useLibraryOffline';
import { pinnedStatusDisplay } from './pinnedStatusDisplay';

type QueueActions = {
  playNext: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
};

function offlineItem(trackId: TrackId, offline: LibraryOffline): ContextMenuItem {
  const { label, action } = pinnedStatusDisplay(offline.statusOf(trackId)).menu;
  if (action === 'unpin') return { label, onPress: () => offline.unpin(trackId) };
  return {
    label,
    onPress: () => {
      if (offline.pin(trackId) === 'storage-full') reportStorageFull();
    },
  };
}

export function buildTrackMenuItems(
  track: TrackResponse,
  opts: {
    offline: LibraryOffline;
    queue: QueueActions;
    onViewDetails: () => void;
    onReacquire?: () => void;
    /** True while this track's re-acquire request is in flight. */
    reacquiring?: boolean;
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
    ...(ready && opts.offline.supported ? [offlineItem(track.id, opts.offline)] : []),
    ...(ready && opts.onReacquire
      ? [
          opts.reacquiring
            ? { label: 'Re-acquiring…', disabled: true, onPress: () => undefined }
            : { label: 'Re-acquire audio', onPress: opts.onReacquire },
        ]
      : []),
    { label: 'View Details', onPress: opts.onViewDetails },
    { label: opts.danger.label, tone: 'danger' as const, onPress: opts.danger.onPress },
  ];
}
