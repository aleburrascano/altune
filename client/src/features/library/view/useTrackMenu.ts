import { useCallback, useState } from 'react';
import type { TrackResponse } from '@shared/catalog';
import type { useQueuePlayback } from '@shared/playback';
import type { ContextMenuItem, MenuAnchor } from '@shared/ui';
import { useLibraryOffline } from '@features/library/view/useLibraryOffline';
import type { ShowFailure } from '@features/library/core/errors';
import { useReacquireTrack } from '@features/library/view/useRetryAcquisition';
import { buildTrackMenuItems, type DangerAction } from '@features/library/view/trackMenuItems';

type TrackAction = { track: TrackResponse; anchor: MenuAnchor };

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
