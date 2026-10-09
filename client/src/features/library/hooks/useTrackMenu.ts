import { useCallback, useState } from 'react';
import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { toPlaybackTrack, type PlaybackTrack } from '@shared/playback/playbackTrack';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';
import {
  reportStorageFull,
  useLibraryOffline,
  type LibraryOffline,
} from '@features/library/hooks/useLibraryOffline';
import { pinnedStatusDisplay } from '@features/library/domain/pinnedStatusDisplay';
import { useReacquireTrack } from '@features/library/hooks/useRetryAcquisition';

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
  if (ready && opts.offline.supported) items.push(offlineItem(track.id, opts.offline));
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
