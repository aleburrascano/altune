import { useCallback, useState } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { useSelection, type Selection } from '../useSelection';
import type { SelectionAction } from '../ui/SelectionBar';
import { buildSelectionActions } from '../ui/selectionActions';
import { buildTrackMenuItems } from '../ui/trackMenu';
import { useReacquireTrack } from './useReacquireTrack';

type TrackAction = { track: TrackResponse; anchor: MenuAnchor };

export type TrackSelectionOptions = {
  queue: ReturnType<typeof useQueuePlayback>;
  onViewDetails: (track: TrackResponse) => void;
  onAddTrackToPlaylist?: (track: TrackResponse) => void;
  trackDanger: (track: TrackResponse) => { label: string; onPress: () => void };
  selectionDanger: {
    label: string;
    onRemove: (ids: TrackId[], clear: () => void) => void;
  };
};

export type TrackSelectionController = {
  selection: Selection;
  onTrackMore: (track: TrackResponse, anchor: MenuAnchor) => void;
  trackAction: TrackAction | null;
  closeTrackMenu: () => void;
  trackMenuItems: (track: TrackResponse) => ContextMenuItem[];
  bulkSheetVisible: boolean;
  closeBulkSheet: () => void;
  selectionActionsFor: (tracks: TrackResponse[]) => SelectionAction[];
  allSelected: (tracks: TrackResponse[]) => boolean;
  toggleSelectAll: (tracks: TrackResponse[]) => void;
};

/**
 * Owns the shared track-selection wiring — selection state, the track-action
 * menu, the bulk-add sheet, and the built selection/menu actions — so a screen
 * supplies only its data source and its danger actions. The action/select-all
 * builders take `tracks` as a parameter (rather than the hook) so callers whose
 * track list is derived after selection is created — e.g. LibraryScreen, where
 * useActiveLibraryView consumes `selection` — keep a stable, unconditional hook
 * order. TrackSelectionOverlay drives them at render.
 */
export function useTrackSelection(opts: TrackSelectionOptions): TrackSelectionController {
  const selection = useSelection();
  const reacquire = useReacquireTrack();
  const pinnedEntries = usePinnedStore((s) => s.entries);
  const pinMany = usePinnedStore((s) => s.pinMany);
  const unpin = usePinnedStore((s) => s.unpin);

  const [trackAction, setTrackAction] = useState<TrackAction | null>(null);
  const [bulkSheetVisible, setBulkSheetVisible] = useState(false);

  const onTrackMore = useCallback(
    (track: TrackResponse, anchor: MenuAnchor) => setTrackAction({ track, anchor }),
    [],
  );
  const closeTrackMenu = useCallback(() => setTrackAction(null), []);
  const closeBulkSheet = useCallback(() => {
    setBulkSheetVisible(false);
    selection.clear();
  }, [selection]);

  const trackMenuItems = (track: TrackResponse): ContextMenuItem[] =>
    buildTrackMenuItems(track, {
      onReacquire: () => reacquire.mutate(track.id),
      queue: opts.queue,
      onViewDetails: () => opts.onViewDetails(track),
      ...(opts.onAddTrackToPlaylist
        ? { onAddToPlaylist: () => opts.onAddTrackToPlaylist?.(track) }
        : {}),
      danger: opts.trackDanger(track),
    });

  const selectionActionsFor = (tracks: TrackResponse[]): SelectionAction[] =>
    buildSelectionActions(
      tracks.filter((t) => selection.has(t.id)),
      {
        pinnedEntries,
        pinMany,
        unpin,
        queue: opts.queue,
        onAddToPlaylist: () => setBulkSheetVisible(true),
        onDone: selection.clear,
        danger: {
          label: opts.selectionDanger.label,
          onPress: () => opts.selectionDanger.onRemove(selection.ids, selection.clear),
        },
      },
    );

  const allSelected = (tracks: TrackResponse[]): boolean => selection.count === tracks.length;

  const toggleSelectAll = (tracks: TrackResponse[]): void => {
    if (selection.count === tracks.length) {
      selection.clear();
    } else {
      selection.selectAll(tracks.map((t) => t.id));
    }
  };

  return {
    selection,
    onTrackMore,
    trackAction,
    closeTrackMenu,
    trackMenuItems,
    bulkSheetVisible,
    closeBulkSheet,
    selectionActionsFor,
    allSelected,
    toggleSelectAll,
  };
}
