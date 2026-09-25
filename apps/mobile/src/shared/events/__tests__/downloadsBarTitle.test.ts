import { QueryClient } from '@tanstack/react-query';

import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { asTrackId } from '@shared/api-client/ids';

import { applyServerEvent } from '../applyServerEvent';
import type { ServerEvent } from '../sse-client';

// Covers the downloads bar title bug: `track_acquisition_started`/`failed` used to
// find nothing but a bare trackId whenever no library query had ever been cached
// this session, so the bar fell back to a literal "track". `track_added_to_library`
// always carries the real metadata first, so the entry should get it from there
// instead of the (possibly empty) query caches.
function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function serverEvent(type: string, data: Record<string, unknown> = {}): ServerEvent {
  return { id: '1', type, data };
}

function addedToLibraryEvent(overrides: Record<string, unknown> = {}): ServerEvent {
  return serverEvent('track_added_to_library', {
    id: 't1',
    title: 'Song Title',
    artist: 'The Artist',
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'pending',
    album: null,
    duration_seconds: null,
    artwork_url: 'https://cdn/art.png',
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    ...overrides,
  });
}

beforeEach(() => {
  useTrackStatusStore.getState().reset();
  useDownloadStore.getState().reset();
});

afterEach(() => {
  useDownloadStore.getState().reset();
});

describe('added -> started with no library query ever cached', () => {
  it('gives the started entry the real title instead of falling back to null', () => {
    const queryClient = makeClient();

    applyServerEvent(queryClient, addedToLibraryEvent());
    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));

    const entry = useDownloadStore.getState().entries[asTrackId('t1')];
    expect(entry?.title).toBe('Song Title');
    expect(entry?.artist).toBe('The Artist');
    expect(entry?.artworkUrl).toBe('https://cdn/art.png');
  });
});

describe('added -> failed (refused save) with no `started` in between', () => {
  it('gives the failed entry the real title', () => {
    const queryClient = makeClient();

    applyServerEvent(queryClient, addedToLibraryEvent());
    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_failed', { track_id: 't1', reason: 'no_candidates' }),
    );

    const entry = useDownloadStore.getState().entries[asTrackId('t1')];
    expect(entry?.phase).toBe('failed');
    expect(entry?.title).toBe('Song Title');
  });
});

describe('added_to_library alone', () => {
  it('does not create a visible download entry', () => {
    const queryClient = makeClient();

    applyServerEvent(queryClient, addedToLibraryEvent());

    expect(useDownloadStore.getState().entries[asTrackId('t1')]).toBeUndefined();
  });
});

describe('reset', () => {
  it('clears remembered metadata along with active entries', () => {
    const queryClient = makeClient();

    applyServerEvent(queryClient, addedToLibraryEvent());
    useDownloadStore.getState().reset();
    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));

    expect(useDownloadStore.getState().entries[asTrackId('t1')]?.title).toBeNull();
  });
});
