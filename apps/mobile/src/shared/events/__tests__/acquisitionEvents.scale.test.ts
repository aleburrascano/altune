import { QueryClient, type InfiniteData } from '@tanstack/react-query';

import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

import { applyServerEvent } from '../applyServerEvent';
import type { ServerEvent } from '../sse-client';
import { settleTrackPatches } from './settleTrackPatches';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn().mockResolvedValue([]) }));

// A library the size a real one reaches, all of it cached with staleTime: Infinity, so a
// pass over it is the 5,000-element scan a bulk import used to pay for on every event.
const PAGE_COUNT = 25;
const PAGE_SIZE = 200;
const LIBRARY_SIZE = PAGE_COUNT * PAGE_SIZE;
const IMPORTED_TRACKS = 60;

const LIBRARY_KEY = libraryKeys.tracks('', 'recent');

type TrackPages = InfiniteData<ListTracksResponse>;
type RowReads = { count: number };

function trackFixture(id: string, overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: asTrackId(id),
    title: `Title ${id}`,
    artist: 'Artist',
    album: null,
    duration_seconds: null,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'pending',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    ...overrides,
  } as TrackResponse;
}

// Every pass over a cached list reads each row's id once to decide whether the row is the
// patched one, so an id getter counts array-element visits without touching the subject.
function countedTrack(id: string, reads: RowReads): TrackResponse {
  return Object.defineProperty(trackFixture(id), 'id', {
    enumerable: true,
    configurable: true,
    get: () => {
      reads.count += 1;
      return id;
    },
  });
}

function rowIds(from: number, count: number): string[] {
  return Array.from({ length: count }, (_, i) => `row${from + i}`);
}

function listResponse(items: TrackResponse[], offset: number): ListTracksResponse {
  return {
    items,
    total: LIBRARY_SIZE,
    limit: PAGE_SIZE,
    offset,
    has_more: offset + items.length < LIBRARY_SIZE,
  };
}

function seedCaches(client: QueryClient, reads: RowReads): void {
  const pages = Array.from({ length: PAGE_COUNT }, (_, page) =>
    listResponse(
      rowIds(page * PAGE_SIZE, PAGE_SIZE).map((id) => countedTrack(id, reads)),
      page * PAGE_SIZE,
    ),
  );
  client.setQueryData<TrackPages>(LIBRARY_KEY, { pages, pageParams: pages.map((_, i) => i) });
  client.setQueryData<ListTracksResponse>(
    libraryKeys.lookup('q'),
    listResponse(
      rowIds(0, PAGE_SIZE).map((id) => countedTrack(id, reads)),
      0,
    ),
  );
  client.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')), {
    id: asPlaylistId('p1'),
    name: 'Imported',
    track_count: PAGE_SIZE,
    preview_artwork_urls: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks: rowIds(0, PAGE_SIZE).map((id) => countedTrack(id, reads)),
  });
}

// The ids the import touches, spread across the cached pages rather than bunched in one.
const importedIds = Array.from({ length: IMPORTED_TRACKS }, (_, i) => `row${i * 83}`);

function completedEvent(trackId: string, index: number): ServerEvent {
  return {
    id: String(index),
    type: 'track_acquisition_completed',
    data: { track_id: trackId, audio_ref: `ref-${trackId}` },
  };
}

function newClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function readLibraryRow(client: QueryClient, trackId: string): TrackResponse | undefined {
  return client
    .getQueryData<TrackPages>(LIBRARY_KEY)!
    .pages.flatMap((page) => page.items)
    .find((t) => t.id === trackId);
}

beforeEach(() => {
  useTrackStatusStore.getState().reset();
  useDownloadStore.getState().reset();
});

describe('a bulk import arriving as one burst of acquisition events', () => {
  it('visits each cached row once for the whole burst rather than once per event', async () => {
    const batchedReads: RowReads = { count: 0 };
    const perEventReads: RowReads = { count: 0 };
    const batched = newClient();
    const perEvent = newClient();
    seedCaches(batched, batchedReads);
    seedCaches(perEvent, perEventReads);

    importedIds.forEach((id, i) => applyServerEvent(batched, completedEvent(id, i)));
    await settleTrackPatches();
    for (const [i, id] of importedIds.entries()) {
      applyServerEvent(perEvent, completedEvent(id, i));
      await settleTrackPatches();
    }

    const [batchedVisits, perEventVisits] = [batchedReads.count, perEventReads.count];
    expect(perEventVisits).toBeGreaterThan(IMPORTED_TRACKS * LIBRARY_SIZE);
    expect(batchedVisits * (IMPORTED_TRACKS / 2)).toBeLessThan(perEventVisits);
    expect(readLibraryRow(batched, 'row83')).toEqual(readLibraryRow(perEvent, 'row83'));
  });

  it('writes the paged library cache once, whatever the number of events in the burst', async () => {
    const client = newClient();
    seedCaches(client, { count: 0 });
    const cache = client.getQueryCache();
    const libraryHash = cache.find({ queryKey: LIBRARY_KEY })!.queryHash;
    let libraryWrites = 0;
    const unsubscribe = cache.subscribe((event) => {
      if (event.type !== 'updated' || event.query.queryHash !== libraryHash) return;
      if (event.action.type === 'success') libraryWrites += 1;
    });

    importedIds.forEach((id, i) => applyServerEvent(client, completedEvent(id, i)));
    await settleTrackPatches();
    unsubscribe();

    expect(libraryWrites).toBe(1);
    expect(readLibraryRow(client, 'row166')?.acquisition_status).toBe('ready');
    expect(readLibraryRow(client, 'row166')?.audio_ref).toBe('ref-row166');
  });
});

// The burst above is all completions because those are the events that only patch. The
// started, progress and failed handlers also read the cache through getTrackFromCaches,
// which is its own per-event scan and outside this ticket.
describe('a mixed burst of acquisition events', () => {
  const t1 = 'row1';
  const t2 = 'row2';

  function mixedEvents(): ServerEvent[] {
    return [
      { id: 'a', type: 'track_acquisition_started', data: { track_id: t1 } },
      { id: 'b', type: 'track_acquisition_progress', data: { track_id: t1, stage: 'download' } },
      { id: 'c', type: 'track_replace_failed', data: { track_id: t1, reason: 'no_source' } },
      {
        id: 'd',
        type: 'track_acquisition_failed',
        data: { track_id: t1, reason: 'no_candidates', failure_message: 'nothing matched' },
      },
      // Carries no message of its own, so it keeps the one the event before it cached —
      // a read of a patch that, batched, has not reached the cache yet.
      { id: 'e', type: 'track_acquisition_failed', data: { track_id: t1, reason: 'no_source' } },
      { id: 'f', type: 'track_acquisition_started', data: { track_id: t2 } },
      { id: 'g', type: 'track_acquisition_completed', data: { track_id: t2, audio_ref: 'ref-2' } },
      // A patch for a track no cache holds yet, ahead of the event that adds it.
      {
        id: 'h',
        type: 'track_acquisition_completed',
        data: { track_id: 'row9000', audio_ref: 'ref-9000' },
      },
      { id: 'k', type: 'track_added_to_library', data: trackFixture('row9000') },
      { id: 'i', type: 'track_acquisition_started', data: { track_id: 'row3' } },
      { id: 'j', type: 'track_deleted', data: { track_id: 'row3' } },
    ];
  }

  function seedSmallCaches(client: QueryClient): void {
    const items = rowIds(1, 3).map((id) => trackFixture(id));
    client.setQueryData<TrackPages>(LIBRARY_KEY, {
      pages: [{ items, total: 3, limit: 20, offset: 0, has_more: false }],
      pageParams: [0],
    });
    client.setQueryData<ListTracksResponse>(libraryKeys.lookup('q'), {
      items: rowIds(1, 3).map((id) => trackFixture(id)),
      total: 3,
      limit: 20,
      offset: 0,
      has_more: false,
    });
  }

  it('settles to the cache that applying each event in turn produces', async () => {
    const batched = newClient();
    const perEvent = newClient();
    seedSmallCaches(batched);
    seedSmallCaches(perEvent);

    mixedEvents().forEach((event) => applyServerEvent(batched, event));
    await settleTrackPatches();
    for (const event of mixedEvents()) {
      applyServerEvent(perEvent, event);
      await settleTrackPatches();
    }

    expect(batched.getQueryData<TrackPages>(LIBRARY_KEY)).toEqual(
      perEvent.getQueryData<TrackPages>(LIBRARY_KEY),
    );
    expect(batched.getQueryData(libraryKeys.lookup('q'))).toEqual(
      perEvent.getQueryData(libraryKeys.lookup('q')),
    );
    expect(readLibraryRow(batched, t1)?.failure_message).toBe('nothing matched');
    expect(readLibraryRow(batched, t1)?.failure_reason).toBe('no_source');
    expect(readLibraryRow(batched, t2)?.acquisition_status).toBe('ready');
    expect(readLibraryRow(batched, 'row3')).toBeUndefined();
    expect(readLibraryRow(batched, 'row9000')?.audio_ref).toBeNull();
  });
});
