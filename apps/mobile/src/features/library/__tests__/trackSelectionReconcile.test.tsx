// Regression for issue #791: the track selection outlived the list it was made
// against. Selecting tracks, then editing the search query (a new visible list)
// left stale ids in the selection, so the header count, "select all" and the
// bulk actions disagreed with what was actually on screen.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { useEffect, type ReactElement } from 'react';

import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import {
  useTrackSelection,
  type TrackSelectionController,
  type TrackSelectionOptions,
} from '../hooks/useTrackSelection';
import { TrackSelectionOverlay } from '../ui/TrackSelectionOverlay';

const mockSheetResolvers: { label: string; resolve: () => Promise<TrackId[]> }[] = [];

jest.mock('@shared/playlists', () => ({
  AddToPlaylistSheet: (props: { label: string; resolveTrackIds: () => Promise<TrackId[]> }) => {
    mockSheetResolvers.push({ label: props.label, resolve: props.resolveTrackIds });
    return null;
  },
}));

function track(id: string): TrackResponse {
  return {
    id: asTrackId(id),
    title: `Track ${id}`,
    artist: 'An Artist',
    album: null,
    duration_seconds: 180,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

const onRemove = jest.fn();
const addToQueue = jest.fn();

let controller: TrackSelectionController;

function Harness({
  tracks,
  expose,
}: {
  tracks: TrackResponse[];
  expose: (c: TrackSelectionController) => void;
}): ReactElement {
  const current = useTrackSelection({
    queue: { addToQueue } as unknown as TrackSelectionOptions['queue'],
    onViewDetails: jest.fn(),
    trackDanger: () => ({ label: 'Remove', onPress: jest.fn() }),
    selectionDanger: { label: 'Remove', onRemove },
  });
  useEffect(() => expose(current));
  return (
    <TrackSelectionOverlay
      controller={current}
      tracks={tracks}
      barVisible={current.selection.active}
    />
  );
}

function renderWith(tracks: TrackResponse[]) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const ui = (list: TrackResponse[]) => (
    <QueryClientProvider client={client}>
      <Harness
        tracks={list}
        expose={(c) => {
          controller = c;
        }}
      />
    </QueryClientProvider>
  );
  const utils = render(ui(tracks));
  return { rerenderWith: (list: TrackResponse[]) => utils.rerender(ui(list)) };
}

const oldResults = [track('a'), track('b'), track('c')];

function selectAllOf(list: TrackResponse[]): void {
  act(() => controller.selection.selectAll(list.map((t) => t.id)));
}

beforeEach(() => {
  onRemove.mockClear();
  addToQueue.mockClear();
  mockSheetResolvers.length = 0;
});

describe('track selection reconciles against the live track list (#791)', () => {
  it('drops ids that left the list when the search query changes, so the count matches', () => {
    const { rerenderWith } = renderWith(oldResults);
    selectAllOf(oldResults);
    expect(screen.getByTestId('selection-count')).toHaveTextContent('3 selected');

    // New query: only "b" survives, plus a track that was never selected.
    rerenderWith([track('b'), track('d')]);

    expect(screen.getByTestId('selection-count')).toHaveTextContent('1 selected');
    expect(controller.selection.ids).toEqual(['b']);
    expect(controller.allSelected([track('b'), track('d')])).toBe(false);
  });

  it('never reports all-selected from a stale count that happens to equal the new length', () => {
    const { rerenderWith } = renderWith(oldResults);
    selectAllOf(oldResults);

    // Three different tracks: raw count 3 === length 3, but none are selected.
    const unrelated = [track('x'), track('y'), track('z')];
    rerenderWith(unrelated);

    expect(controller.allSelected(unrelated)).toBe(false);
    expect(controller.selection.active).toBe(false);
    expect(screen.queryByTestId('selection-bar')).toBeNull();
  });

  it('bulk remove and the bulk add sheet carry only the ids still on screen', async () => {
    const { rerenderWith } = renderWith(oldResults);
    selectAllOf(oldResults);
    const next = [track('c'), track('d')];
    rerenderWith(next);

    fireEvent.press(screen.getByText('Remove'));
    expect(onRemove).toHaveBeenCalledWith(['c'], expect.any(Function));

    const sheet = mockSheetResolvers[mockSheetResolvers.length - 1]!;
    expect(sheet.label).toBe('1 track');
    await expect(sheet.resolve()).resolves.toEqual(['c']);
  });

  it('select all then deselect all toggles against the live list, not the stale count', () => {
    const { rerenderWith } = renderWith(oldResults);
    // Two selected; the new list also has two tracks but only "a" is one of them.
    selectAllOf([track('a'), track('b')]);
    const next = [track('a'), track('d')];
    rerenderWith(next);

    fireEvent.press(screen.getByTestId('selection-select-all'));
    expect(controller.selection.ids.sort()).toEqual(['a', 'd']);
    expect(screen.getByTestId('selection-count')).toHaveTextContent('2 selected');
    expect(controller.allSelected(next)).toBe(true);

    fireEvent.press(screen.getByTestId('selection-select-all'));
    expect(controller.selection.active).toBe(false);
  });

  it('keeps the selection intact when the list only grows (next page loaded)', () => {
    const { rerenderWith } = renderWith(oldResults);
    selectAllOf(oldResults);
    rerenderWith([...oldResults, track('d')]);

    expect(controller.selection.ids).toEqual(['a', 'b', 'c']);
    expect(screen.getByTestId('selection-count')).toHaveTextContent('3 selected');
  });

  it('an empty list (e.g. a new query still loading) clears the selection', () => {
    const { rerenderWith } = renderWith(oldResults);
    selectAllOf(oldResults);
    rerenderWith([]);

    expect(controller.selection.active).toBe(false);
    expect(controller.allSelected([])).toBe(false);
  });
});
