import { useCallback, useState } from 'react';
import type { TrackId, TrackResponse } from '@shared/catalog';
import { type useQueuePlayback, toPlaybackTrack, type PlaybackTrack } from '@shared/playback';
import type { ContextMenuItem, MenuAnchor } from '@shared/ui';
import {
  reportStorageFull,
  useLibraryOffline,
  type LibraryOffline,
} from '@features/library/view/useLibraryOffline';
import { pinnedStatusDisplay } from '@features/library/domain/pinnedStatusDisplay';
import type { ShowFailure } from '@features/library/domain/libraryFailure';
import { useReacquireTrack } from '@features/library/view/useRetryAcquisition';

type TrackAction = { track: TrackResponse; anchor: MenuAnchor };

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
  if (track.acquisition_status !== 'ready') return [];
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
  const ready = track.acquisition_status === 'ready';
  const items: ContextMenuItem[] = [];
  if (opts.onAddToPlaylist) items.push({ label: 'Add to Playlist', onPress: opts.onAddToPlaylist });
  if (ready && opts.offline.supported) items.push(offlineItem(track.id, opts));
  if (ready) items.push(...reacquireItems(opts));
  return items;
}

export function buildTrackMenuItems(
  track: TrackResponse,
  opts: TrackMenuItemOptions,
): ContextMenuItem[] {
  return [...queueItems(track, opts), ...optionalItems(track, opts), ...closingItems(opts)];
}

function closingItems(opts: TrackMenuItemOptions): ContextMenuItem[] {
  return [
    { label: 'View Details', onPress: opts.onViewDetails },
    { label: opts.danger.label, tone: 'danger', onPress: opts.danger.onPress },
  ];
}

export type TrackMenuOptions = {
  queue: ReturnType<typeof useQueuePlayback>;
  onViewDetails: (track: TrackResponse) => void;
  onAddTrackToPlaylist?: (track: TrackResponse) => void;
  trackDanger: (track: TrackResponse) => DangerAction;
  showFailure: ShowFailure;
};

export type TrackMenuController = {
  onTrackMore: (track: TrackResponse, anchor: MenuAnchor) => void;
  trackAction: TrackAction | null;
  closeTrackMenu: () => void;
  trackMenuItems: (track: TrackResponse) => ContextMenuItem[];
};

function useTrackActionState(): Omit<TrackMenuController, 'trackMenuItems'> {
  const [trackAction, setTrackAction] = useState<TrackAction | null>(null);
  const onTrackMore = useCallback(
    (track: TrackResponse, anchor: MenuAnchor) => setTrackAction({ track, anchor }),
    [],
  );
  const closeTrackMenu = useCallback(() => setTrackAction(null), []);
  return { onTrackMore, trackAction, closeTrackMenu };
}

function reacquireActions(track: TrackResponse, reacquire: ReturnType<typeof useReacquireTrack>) {
  return {
    onReacquire: () => reacquire.mutate(track.id),
    reacquiring: reacquire.isInFlight(track.id),
  };
}

function trackMenuActions(track: TrackResponse, opts: TrackMenuOptions) {
  return {
    queue: opts.queue,
    onViewDetails: () => opts.onViewDetails(track),
    ...(opts.onAddTrackToPlaylist
      ? { onAddToPlaylist: () => opts.onAddTrackToPlaylist?.(track) }
      : {}),
    danger: opts.trackDanger(track),
    showFailure: opts.showFailure,
  };
}

function useTrackMenuItems(opts: TrackMenuOptions): TrackMenuController['trackMenuItems'] {
  const reacquire = useReacquireTrack();
  const offline = useLibraryOffline();
  return (track) =>
    buildTrackMenuItems(track, {
      offline,
      ...reacquireActions(track, reacquire),
      ...trackMenuActions(track, opts),
    });
}

export function useTrackMenu(opts: TrackMenuOptions): TrackMenuController {
  const trackMenuItems = useTrackMenuItems(opts);
  return { ...useTrackActionState(), trackMenuItems };
}
