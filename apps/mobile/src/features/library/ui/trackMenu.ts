import { usePinnedStore } from '@shared/offline/pinnedStore';
import type { TrackResponse } from '@shared/api-client/types';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack } from '@shared/playback/types';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';

type QueueActions = {
  playNext: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
};

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
    onReacquire?: () => void;
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
    ...(ready ? [offlineItem(track.id)] : []),
    ...(ready && opts.onReacquire
      ? [{ label: 'Re-acquire audio', onPress: opts.onReacquire }]
      : []),
    { label: 'View Details', onPress: opts.onViewDetails },
    { label: opts.danger.label, tone: 'danger' as const, onPress: opts.danger.onPress },
  ];
}
