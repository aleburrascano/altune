import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { useEffect, type ReactElement } from 'react';
import { asPlaylistId, asTrackId } from '@shared/apiClient/ids';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import { trackResponseFactory } from '@testing/factories';
import { signInTestUser } from '@testing/signInTestUser';
import {
  useTrackSelection,
  type TrackSelectionController,
  type TrackSelectionOptions,
} from '../hooks/useTrackSelection';
import { TrackSelectionOverlay } from '../ui/TrackSelectionOverlay';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';
const PLAYLISTS = 'GET /v1/playlists';
const ADD_TO_P1 = 'POST /v1/playlists/p1/tracks/batch';

function track(id: string): TrackResponse {
  return trackResponseFactory.build({
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
  });
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
      <FeaturePorts ports={httpFeaturePorts}>
        <Harness
          tracks={list}
          expose={(c) => {
            controller = c;
          }}
        />
      </FeaturePorts>
    </QueryClientProvider>
  );
  const utils = render(ui(tracks));
  return { rerenderWith: (list: TrackResponse[]) => utils.rerender(ui(list)) };
}

const oldResults = [track('a'), track('b'), track('c')];

function selectAllOf(list: TrackResponse[]): void {
  act(() => controller.selection.selectAll(list.map((t) => t.id)));
}

beforeEach(async () => {
  onRemove.mockClear();
  addToQueue.mockClear();
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
});

describe('track selection reconciles against the live track list (#791)', () => {
  it('drops ids that left the list when the search query changes, so the count matches', () => {
    const { rerenderWith } = renderWith(oldResults);
    selectAllOf(oldResults);
    expect(screen.getByTestId('selection-count')).toHaveTextContent('3 selected');

    rerenderWith([track('b'), track('d')]);

    expect(screen.getByTestId('selection-count')).toHaveTextContent('1 selected');
    expect(controller.selection.ids).toEqual(['b']);
    expect(controller.allSelected([track('b'), track('d')])).toBe(false);
  });

  it('never reports all-selected from a stale count that happens to equal the new length', () => {
    const { rerenderWith } = renderWith(oldResults);
    selectAllOf(oldResults);

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

    __http.reply(PLAYLISTS, {
      status: 200,
      json: {
        items: [
          {
            id: asPlaylistId('p1'),
            name: 'Focus',
            track_count: 0,
            preview_artwork_urls: [],
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
          },
        ],
        total: 1,
      },
    });
    __http.reply(ADD_TO_P1, { status: 200, json: {} });
    fireEvent.press(screen.getByText('Add to Playlist'));
    expect(screen.getByText('1 track')).toBeTruthy();
    fireEvent.press(await screen.findByTestId('add-to-playlist-p1'));

    await waitFor(() => expect(__http.countFor(ADD_TO_P1)).toBe(1));
    const add = (__http.requests as { path: string; body?: unknown }[]).find(
      (r) => r.path === '/v1/playlists/p1/tracks/batch',
    );
    expect(JSON.parse(String(add?.body))).toEqual(expect.objectContaining({ track_ids: ['c'] }));
  });

  it('select all then deselect all toggles against the live list, not the stale count', () => {
    const { rerenderWith } = renderWith(oldResults);
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
