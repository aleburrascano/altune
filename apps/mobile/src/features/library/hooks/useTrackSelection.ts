import { useCallback, useState } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import { useSelection, type Selection } from './useSelection';
import type { SelectionAction } from '../selectionActions';
import { buildSelectionActions } from '../selectionActions';
import { useLibraryOffline } from './useLibraryOffline';
import { useTrackMenu, type TrackMenuController, type TrackMenuOptions } from './useTrackMenu';

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

export function useTrackSelection(opts: TrackSelectionOptions): TrackSelectionController {
  const selection = useSelection();
  const menu = useTrackMenu(opts);
  const { openBulkSheet, ...bulkSheet } = useBulkSheet(selection);
  const selectionActionsFor = useSelectionActionsFor({ selection, opts, openBulkSheet });
  return { ...menu, ...bulkSheet, ...selectionQueries(selection), selection, selectionActionsFor };
}

type RemoveSelected = TrackSelectionOptions['selectionDanger'];

type SelectionActionDeps = {
  selection: Selection;
  opts: TrackSelectionOptions;
  openBulkSheet: () => void;
};

function selectedIds(selection: Selection, tracks: TrackResponse[]): TrackId[] {
  return tracks.filter((t) => selection.has(t.id)).map((t) => t.id);
}

function allSelected(selection: Selection, tracks: TrackResponse[]): boolean {
  return tracks.length > 0 && tracks.every((t) => selection.has(t.id));
}

function toggleSelectAll(selection: Selection, tracks: TrackResponse[]): void {
  if (allSelected(selection, tracks)) {
    selection.clear();
  } else {
    selection.selectAll(tracks.map((t) => t.id));
  }
}

function selectionQueries(
  selection: Selection,
): Pick<TrackSelectionController, 'selectedIds' | 'allSelected' | 'toggleSelectAll'> {
  return {
    selectedIds: (tracks) => selectedIds(selection, tracks),
    allSelected: (tracks) => allSelected(selection, tracks),
    toggleSelectAll: (tracks) => toggleSelectAll(selection, tracks),
  };
}

function useBulkSheet(selection: Selection) {
  const [bulkSheetVisible, setBulkSheetVisible] = useState(false);
  const openBulkSheet = useCallback(() => setBulkSheetVisible(true), []);
  const closeBulkSheet = useCallback(() => {
    setBulkSheetVisible(false);
    selection.clear();
  }, [selection]);
  return { bulkSheetVisible, openBulkSheet, closeBulkSheet };
}

function removeSelectedAction(
  selection: Selection,
  tracks: TrackResponse[],
  removeSelected: RemoveSelected,
): { label: string; onPress: () => void } {
  return {
    label: removeSelected.label,
    onPress: () => removeSelected.onRemove(selectedIds(selection, tracks), selection.clear),
  };
}

function selectionActionOptions(deps: SelectionActionDeps, tracks: TrackResponse[]) {
  return {
    queue: deps.opts.queue,
    onAddToPlaylist: deps.openBulkSheet,
    onDone: deps.selection.clear,
    danger: removeSelectedAction(deps.selection, tracks, deps.opts.selectionDanger),
  };
}

function useSelectionActionsFor(
  deps: SelectionActionDeps,
): TrackSelectionController['selectionActionsFor'] {
  const offline = useLibraryOffline();
  return (tracks) =>
    buildSelectionActions(
      tracks.filter((t) => deps.selection.has(t.id)),
      { offline, ...selectionActionOptions(deps, tracks) },
    );
}
