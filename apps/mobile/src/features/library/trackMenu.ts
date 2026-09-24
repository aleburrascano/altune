import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import type { PinAdmission, PinnedEntry } from '@shared/offline/pinnedStore';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack } from '@shared/playback/types';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';

import { reportStorageFull } from './pinBatchSummary';
import { pinnedStatusDisplay } from './pinnedStatusDisplay';

type QueueActions = {
  playNext: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
};

type OfflineActions = {
  pinnedEntries: Record<string, PinnedEntry>;
  pin: (trackId: TrackId) => PinAdmission;
  unpin: (trackId: TrackId) => void;
};

function offlineItem(trackId: TrackId, offline: OfflineActions): ContextMenuItem {
  const { label, action } = pinnedStatusDisplay(offline.pinnedEntries[trackId]?.status).menu;
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
  opts: OfflineActions & {
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
    ...(ready ? [offlineItem(track.id, opts)] : []),
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
