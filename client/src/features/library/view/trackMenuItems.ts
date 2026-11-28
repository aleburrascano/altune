import type { TrackId, TrackResponse } from '@shared/catalog';
import { toPlaybackTrack, type PlaybackTrack } from '@shared/playback';
import type { ContextMenuItem } from '@shared/ui';
import { reportStorageFull, type LibraryOffline } from '@features/library/view/useLibraryOffline';
import { pinnedStatusDisplay } from '@features/library/core/pinnedStatusDisplay';
import type { ShowFailure } from '@features/library/core/errors';
import { isReady } from '@shared/catalog';

export type DangerAction = { label: string; onPress: () => void };

type QueueActions = {
  playNext: (track: PlaybackTrack) => void;
  addToQueue: (track: PlaybackTrack) => void;
};

export type TrackMenuItemOptions = {
  offline: LibraryOffline;
  queue: QueueActions;
  onViewDetails: () => void;
  onReacquire?: () => void;
  reacquiring?: boolean;
  onAddToPlaylist?: () => void;
  danger: DangerAction;
  showFailure: ShowFailure;
};

function pinItem(trackId: TrackId, label: string, opts: TrackMenuItemOptions): ContextMenuItem {
  return {
    label,
    onPress: () => {
      if (opts.offline.pin(trackId) === 'storage-full') reportStorageFull(opts.showFailure);
    },
  };
}

function offlineItem(trackId: TrackId, opts: TrackMenuItemOptions): ContextMenuItem {
  const { offline } = opts;
  const { label, action } = pinnedStatusDisplay(offline.statusOf(trackId)).menu;
  if (action === 'unpin') return { label, onPress: () => offline.unpin(trackId) };
  return pinItem(trackId, label, opts);
}

function queueItems(track: TrackResponse, opts: TrackMenuItemOptions): ContextMenuItem[] {
  if (!isReady(track.acquisition_status)) return [];
  return [
    { label: 'Play Next', onPress: () => opts.queue.playNext(toPlaybackTrack(track)) },
    { label: 'Add to Queue', onPress: () => opts.queue.addToQueue(toPlaybackTrack(track)) },
  ];
}

function reacquireItems(opts: TrackMenuItemOptions): ContextMenuItem[] {
  if (!opts.onReacquire) return [];
  if (opts.reacquiring)
    return [{ label: 'Re-acquiring…', disabled: true, onPress: () => undefined }];
  return [{ label: 'Re-acquire audio', onPress: opts.onReacquire }];
}

function optionalItems(track: TrackResponse, opts: TrackMenuItemOptions): ContextMenuItem[] {
  const ready = isReady(track.acquisition_status);
  const items: ContextMenuItem[] = [];
  if (opts.onAddToPlaylist) items.push({ label: 'Add to Playlist', onPress: opts.onAddToPlaylist });
  if (ready && opts.offline.supported) items.push(offlineItem(track.id, opts));
  if (ready) items.push(...reacquireItems(opts));
  return items;
}

function closingItems(opts: TrackMenuItemOptions): ContextMenuItem[] {
  return [
    { label: 'View Details', onPress: opts.onViewDetails },
    { label: opts.danger.label, tone: 'danger', onPress: opts.danger.onPress },
  ];
}

export function buildTrackMenuItems(
  track: TrackResponse,
  opts: TrackMenuItemOptions,
): ContextMenuItem[] {
  return [...queueItems(track, opts), ...optionalItems(track, opts), ...closingItems(opts)];
}
