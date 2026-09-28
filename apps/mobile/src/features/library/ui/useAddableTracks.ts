import { useMemo } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import { useLibrarySearch } from '../hooks/useLibrarySearch';
import { useLibraryTracks } from '../hooks/useLibraryTracks';
import { useSelection, type Selection } from '../hooks/useSelection';

export type SearchApi = ReturnType<typeof useLibrarySearch>;
export type TracksQuery = ReturnType<typeof useLibraryTracks>;

export type AddableTracksProps = {
  visible: boolean;
  existingTrackIds: TrackId[];
  adding: boolean;
  onAdd: (trackIds: TrackId[]) => void;
  onClose: () => void;
};

type TracksState = {
  search: SearchApi;
  selection: Selection;
  query: TracksQuery;
  existing: Set<TrackId>;
  addable: TrackResponse[];
};

type TracksActions = {
  allSelected: boolean;
  isEmpty: boolean;
  canConfirm: boolean;
  close: () => void;
  confirm: () => void;
};

export type AddableTracksState = TracksState & TracksActions;

function addableOf(tracks: TrackResponse[], existing: Set<TrackId>): TrackResponse[] {
  return tracks.filter((t) => !existing.has(t.id));
}

function closeOf(selection: Selection, search: SearchApi, onClose: () => void): () => void {
  return () => {
    selection.clear();
    search.onClear();
    onClose();
  };
}

function confirmOf(selection: Selection, onAdd: AddableTracksProps['onAdd']): () => void {
  return () => {
    if (selection.count === 0) return;
    onAdd(selection.ids);
  };
}

function useTracksQuery(search: SearchApi, visible: boolean): TracksQuery {
  return useLibraryTracks(search.query, 'recent', visible);
}

function useTracksState(props: AddableTracksProps): TracksState {
  const search = useLibrarySearch();
  const selection = useSelection();
  const query = useTracksQuery(search, props.visible);
  const existing = useMemo(() => new Set(props.existingTrackIds), [props.existingTrackIds]);
  return { search, selection, query, existing, addable: addableOf(query.tracks, existing) };
}

function deriveActions(state: TracksState, props: AddableTracksProps): TracksActions {
  const isEmpty = state.selection.count === 0;
  return {
    allSelected: state.addable.length > 0 && state.selection.count === state.addable.length,
    isEmpty,
    canConfirm: !isEmpty && !props.adding,
    close: closeOf(state.selection, state.search, props.onClose),
    confirm: confirmOf(state.selection, props.onAdd),
  };
}

export function useAddableTracks(props: AddableTracksProps): AddableTracksState {
  const state = useTracksState(props);
  return { ...state, ...deriveActions(state, props) };
}
