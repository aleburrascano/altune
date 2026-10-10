import { useCallback, useState } from 'react';
import { Download, ListEnd, ListPlus, Trash2, XCircle, type LucideIcon } from 'lucide-react-native';
import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { toPlaybackTrack, type PlaybackTrack } from '@shared/playback/playbackTrack';
import { useSelection, type Selection } from '@features/library/hooks/useSelection';
import {
  offlineEligibility,
  reportPinBatch,
  reportUnpinBatch,
  useLibraryOffline,
  type LibraryOffline,
} from '@features/library/hooks/useLibraryOffline';
import {
  useTrackMenu,
  type DangerAction,
  type TrackMenuController,
  type TrackMenuOptions,
} from '@features/library/hooks/useTrackMenu';

export type SelectionAction = {
  key: string;
  label: string;
  icon: LucideIcon;
  onPress: () => void;
  tone?: 'danger';
  disabled?: boolean;
};

type SelectionQueue = { addToQueueMany: (tracks: readonly PlaybackTrack[]) => void };

export type SelectionActionOptions = {
  offline: LibraryOffline;
  queue: SelectionQueue;
  onAddToPlaylist: () => void;
  onDone: () => void;
  danger: DangerAction;
};

type OfflineEligibility = ReturnType<typeof offlineEligibility>;

function playlistAction(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction {
  return {
    key: 'playlist',
    label: 'Add to Playlist',
    icon: ListPlus,
    disabled: selected.length === 0,
    onPress: opts.onAddToPlaylist,
  };
}

function pressOffline(eligibility: OfflineEligibility, opts: SelectionActionOptions): void {
  const { downloadableIds, allPinned } = eligibility;
  if (allPinned) void opts.offline.unpinMany(downloadableIds).then(reportUnpinBatch);
  else void opts.offline.pinMany(downloadableIds).then(reportPinBatch);
  opts.onDone();
}

function offlineAction(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction {
  const eligibility = offlineEligibility(selected, opts.offline.statusOf);
  return {
    key: 'offline',
    label: eligibility.allPinned ? 'Remove download' : 'Download',
    icon: eligibility.allPinned ? XCircle : Download,
    disabled: eligibility.downloadableIds.length === 0,
    onPress: () => pressOffline(eligibility, opts),
  };
}

function offlineActions(
  selected: TrackResponse[],
  opts: SelectionActionOptions,
): SelectionAction[] {
  if (!opts.offline.supported) return [];
  return [offlineAction(selected, opts)];
}

function queueReady(ready: TrackResponse[], opts: SelectionActionOptions): void {
  opts.queue.addToQueueMany(ready.map((t) => toPlaybackTrack(t)));
  opts.onDone();
}

function queueAction(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction {
  const ready = selected.filter((t) => t.acquisition_status === 'ready');
  return {
    key: 'queue',
    label: 'Add to Queue',
    icon: ListEnd,
    disabled: ready.length === 0,
    onPress: () => queueReady(ready, opts),
  };
}

function dangerAction(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction {
  return {
    key: 'danger',
    label: opts.danger.label,
    icon: Trash2,
    tone: 'danger',
    disabled: selected.length === 0,
    onPress: opts.danger.onPress,
  };
}

function leadActions(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction[] {
  return [playlistAction(selected, opts), ...offlineActions(selected, opts)];
}

function tailActions(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction[] {
  return [queueAction(selected, opts), dangerAction(selected, opts)];
}

export function buildSelectionActions(
  selected: TrackResponse[],
  opts: SelectionActionOptions,
): SelectionAction[] {
  return [...leadActions(selected, opts), ...tailActions(selected, opts)];
}

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
  const ids: TrackId[] = [];
  for (const t of tracks) {
    if (selection.has(t.id)) ids.push(t.id);
  }
  return ids;
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
): DangerAction {
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
