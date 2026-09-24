import { useCallback, useEffect, useState } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { useSelection, type Selection } from './useSelection';
import type { SelectionAction } from '../ui/SelectionBar';
import { buildSelectionActions } from '../selectionActions';
import { buildTrackMenuItems } from '../trackMenu';
import { useReacquireTrack } from './useReacquireTrack';

type TrackAction = { track: TrackResponse; anchor: MenuAnchor };

export type TrackMenuOptions = {
  queue: ReturnType<typeof useQueuePlayback>;
  onViewDetails: (track: TrackResponse) => void;
  onAddTrackToPlaylist?: (track: TrackResponse) => void;
  trackDanger: (track: TrackResponse) => { label: string; onPress: () => void };
};

export type TrackMenuController = {
  onTrackMore: (track: TrackResponse, anchor: MenuAnchor) => void;
  trackAction: TrackAction | null;
  closeTrackMenu: () => void;
  trackMenuItems: (track: TrackResponse) => ContextMenuItem[];
};

export type TrackSelectionOptions = TrackMenuOptions & {
  selectionDanger: {
    label: string;
    onRemove: (ids: TrackId[], clear: () => void) => void;
  };
};

export type TrackSelectionController = TrackMenuController & {
  selection: Selection;
  bulkSheetVisible: boolean;
  closeBulkSheet: () => void;
  selectedIds: (tracks: TrackResponse[]) => TrackId[];
  selectionActionsFor: (tracks: TrackResponse[]) => SelectionAction[];
  allSelected: (tracks: TrackResponse[]) => boolean;
  toggleSelectAll: (tracks: TrackResponse[]) => void;
};

/**
 * The single-track action menu on its own: which track/anchor is open and the
 * items built for it. Screens without bulk selection consume this directly;
 * useTrackSelection composes it.
 */
export function useTrackMenu(opts: TrackMenuOptions): TrackMenuController {
  const reacquire = useReacquireTrack();
  const pinnedEntries = usePinnedStore((s) => s.entries);
  const pin = usePinnedStore((s) => s.pin);
  const unpin = usePinnedStore((s) => s.unpin);
  const [trackAction, setTrackAction] = useState<TrackAction | null>(null);

  const onTrackMore = useCallback(
    (track: TrackResponse, anchor: MenuAnchor) => setTrackAction({ track, anchor }),
    [],
  );
  const closeTrackMenu = useCallback(() => setTrackAction(null), []);

  const trackMenuItems = (track: TrackResponse): ContextMenuItem[] =>
    buildTrackMenuItems(track, {
      pinnedEntries,
      pin,
      unpin,
      onReacquire: () => reacquire.mutate(track.id),
      reacquiring: reacquire.isInFlight(track.id),
      queue: opts.queue,
      onViewDetails: () => opts.onViewDetails(track),
      ...(opts.onAddTrackToPlaylist
        ? { onAddToPlaylist: () => opts.onAddTrackToPlaylist?.(track) }
        : {}),
      danger: opts.trackDanger(track),
    });

  return { onTrackMore, trackAction, closeTrackMenu, trackMenuItems };
}

/**
 * Owns the shared track-selection wiring — selection state, the track-action
 * menu, the bulk-add sheet, and the built selection/menu actions — so a screen
 * supplies only its data source and its danger actions. The action/select-all
 * builders take `tracks` as a parameter (rather than the hook) so callers whose
 * track list is derived after selection is created — e.g. LibraryScreen, where
 * useActiveLibraryView consumes `selection` — keep a stable, unconditional hook
 * order. TrackSelectionOverlay drives them at render.
 *
 * Every count, select-all check and bulk action is derived from the selection
 * intersected with the live `tracks`, so a selection made against an earlier
 * list (before a search edit, a chip switch, or a removal) never leaks stale
 * ids into what the bar shows or acts on. useReconcileSelection then prunes the
 * stored state to match.
 */
export function useTrackSelection(opts: TrackSelectionOptions): TrackSelectionController {
  const selection = useSelection();
  const menu = useTrackMenu(opts);
  const pinnedEntries = usePinnedStore((s) => s.entries);
  const pinMany = usePinnedStore((s) => s.pinMany);
  const unpinMany = usePinnedStore((s) => s.unpinMany);

  const [bulkSheetVisible, setBulkSheetVisible] = useState(false);

  const closeBulkSheet = useCallback(() => {
    setBulkSheetVisible(false);
    selection.clear();
  }, [selection]);

  const selectedIds = (tracks: TrackResponse[]): TrackId[] =>
    tracks.filter((t) => selection.has(t.id)).map((t) => t.id);

  const selectionActionsFor = (tracks: TrackResponse[]): SelectionAction[] =>
    buildSelectionActions(
      tracks.filter((t) => selection.has(t.id)),
      {
        pinnedEntries,
        pinMany,
        unpinMany,
        queue: opts.queue,
        onAddToPlaylist: () => setBulkSheetVisible(true),
        onDone: selection.clear,
        danger: {
          label: opts.selectionDanger.label,
          onPress: () => opts.selectionDanger.onRemove(selectedIds(tracks), selection.clear),
        },
      },
    );

  const allSelected = (tracks: TrackResponse[]): boolean =>
    tracks.length > 0 && tracks.every((t) => selection.has(t.id));

  const toggleSelectAll = (tracks: TrackResponse[]): void => {
    if (allSelected(tracks)) {
      selection.clear();
    } else {
      selection.selectAll(tracks.map((t) => t.id));
    }
  };

  return {
    ...menu,
    selection,
    bulkSheetVisible,
    closeBulkSheet,
    selectedIds,
    selectionActionsFor,
    allSelected,
    toggleSelectAll,
  };
}

/**
 * Prunes a selection down to the ids still present in the live `tracks` list,
 * clearing it outright when none survive. Runs whenever the list backing the
 * selection changes, so selection mode never lingers over tracks that are no
 * longer on screen. Growth (a next page loading) keeps the selection intact.
 */
export function useReconcileSelection(selection: Selection, tracks: TrackResponse[]): void {
  const { ids, clear, selectAll } = selection;
  const idsKey = ids.join('\n');

  useEffect(() => {
    if (idsKey === '') return;
    const live = new Set<string>(tracks.map((t) => t.id));
    const current = idsKey.split('\n') as TrackId[];
    const kept = current.filter((id) => live.has(id));
    if (kept.length === current.length) return;
    if (kept.length === 0) clear();
    else selectAll(kept);
  }, [idsKey, tracks, clear, selectAll]);
}
