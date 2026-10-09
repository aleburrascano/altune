import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { QueryClient } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { asTrackId } from '@shared/api-client/ids';
import type { CreateTrackRequest, TrackResponse } from '@shared/api-client/trackResponse';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { libraryKeys } from '@shared/query/queryKeys';
import { readDetailHandoff } from '@shared/navigation/detailHandoff';

import { useAlbumDetailState } from '@/features/detail/hooks/useAlbumDetailState';
import { SAVE_ALL_CONCURRENCY } from '@/features/detail/domain/saveTrack';
import { LOOKUP_LIMIT, LOOKUP_MAX_PAGES } from '@/features/detail/hooks/useLibraryMatches';
import type { DetailPort } from '@/features/detail/ports/detailPort';
import {
  createMemoryDetailPort,
  type MemoryDetailSeed,
} from '@/features/detail/__tests__/memoryDetailPort';
import {
  createPortWrapper,
  createTestQueryClient,
} from '@/features/detail/__tests__/support/queryHarness';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({ playFromList: jest.fn() }),
}));
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

const albumResult: DiscoveryResult = {
  kind: 'album',
  title: 'Album',
  subtitle: 'Artist',
  image_url: null,
  confidence: 'high',
  sources: [{ provider: 'deezer', external_id: 'alb1', url: 'https://deezer/alb1' }],
  extras: {},
};

function unownedTrack(i: number, extras: Record<string, unknown> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: `Track ${i}`,
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras,
  };
}

function libraryTrack(i: number): TrackResponse {
  return {
    id: asTrackId(`lib-${i}`),
    title: `Elsewhere ${i}`,
    artist: 'Someone',
    album: 'Another Album',
    duration_seconds: null,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
  } as TrackResponse;
}

const LARGE_ALBUM_SIZE = 20;
const MAX_ROUNDS = 50;

type Deferred = { resolve: () => void; reject: (e: unknown) => void };

function savingPort(seed: MemoryDetailSeed) {
  const memory = createMemoryDetailPort(seed);
  let active = 0;
  let maxConcurrent = 0;
  const bodies: CreateTrackRequest[] = [];
  const pending: Deferred[] = [];
  const port: DetailPort = {
    ...memory,
    createTrack: (body, idempotencyKey) => {
      bodies.push(body);
      active += 1;
      maxConcurrent = Math.max(maxConcurrent, active);
      return new Promise<TrackResponse>((resolve, reject) => {
        pending.push({
          resolve: () => {
            active -= 1;
            resolve(memory.createTrack(body, idempotencyKey));
          },
          reject: (e) => {
            active -= 1;
            reject(e);
          },
        });
      });
    },
  };
  return {
    port,
    pending,
    get started() {
      return bodies.length;
    },
    get maxConcurrent() {
      return maxConcurrent;
    },
    savedTitles: () => bodies.map((b) => b.title),
    bodies,
  };
}

function albumSeed(tracks: DiscoveryResult[], seed: MemoryDetailSeed = {}): MemoryDetailSeed {
  return {
    ...seed,
    albumTracks: { Album: { items: tracks, provider_name: 'memory', status: 'ok' } },
  };
}

async function tick(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

async function lookupSettled(client: QueryClient): Promise<void> {
  await waitFor(() =>
    expect(['success', 'error']).toContain(
      client.getQueryState(libraryKeys.lookup('Album'))?.status,
    ),
  );
}

async function renderAlbum(port: DetailPort, album: DiscoveryResult = albumResult, count = 0) {
  const client = createTestQueryClient({ mutations: true });
  const hook = renderHook(() => useAlbumDetailState(album, '/discover/detail'), {
    wrapper: createPortWrapper(client, { detail: port }),
  });
  await waitFor(() => expect(hook.result.current.tracks).toHaveLength(count));
  await lookupSettled(client);
  return { ...hook, client };
}

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  useTrackStatusStore.getState().reset();
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warnSpy.mockRestore();
  useTrackStatusStore.getState().reset();
});

describe('useAlbumDetailState — onSaveAll', () => {
  it('bounds concurrent save requests instead of firing one per unowned track at once', async () => {
    const tracks = Array.from({ length: LARGE_ALBUM_SIZE }, (_, i) => unownedTrack(i));
    const dbl = savingPort(albumSeed(tracks));
    const { result } = await renderAlbum(dbl.port, albumResult, tracks.length);

    act(() => result.current.onSaveAll());
    for (let guard = 0; guard < MAX_ROUNDS; guard += 1) {
      await tick();
      if (dbl.started === tracks.length && dbl.pending.length === 0) break;
      await act(async () => dbl.pending.splice(0).forEach((d) => d.resolve()));
    }

    expect(dbl.started).toBe(tracks.length);
    expect(dbl.maxConcurrent).toBe(SAVE_ALL_CONCURRENCY);
    await waitFor(() => expect(result.current.savingAll).toBe(false));
    expect(result.current.lastBatch).toEqual({ saved: tracks.length, failed: 0 });
  });

  it('recovers the control after a partial failure rather than wedging on Saving… forever', async () => {
    const tracks = [unownedTrack(0), unownedTrack(1), unownedTrack(2)];
    const dbl = savingPort(albumSeed(tracks));
    const { result } = await renderAlbum(dbl.port, albumResult, tracks.length);

    act(() => result.current.onSaveAll());
    await waitFor(() => expect(dbl.pending).toHaveLength(tracks.length));

    expect(result.current.savingAll).toBe(true);

    await act(async () => {
      dbl.pending[1]!.reject(new Error('save failed'));
      dbl.pending[0]!.resolve();
      dbl.pending[2]!.resolve();
    });

    await waitFor(() => expect(result.current.savingAll).toBe(false));
    expect(result.current.lastBatch).toEqual({ saved: 2, failed: 1 });
    expect(result.current.owned.unownedCount).toBe(tracks.length);
  });

  it('saves a track tagged with an empty album under the containing album instead', async () => {
    const dbl = savingPort(albumSeed([unownedTrack(0, { album: '', album_artist: '' })]));
    const { result } = await renderAlbum(dbl.port, albumResult, 1);

    act(() => result.current.onSaveAll());
    await waitFor(() => expect(dbl.pending).toHaveLength(1));
    await act(async () => dbl.pending.splice(0).forEach((d) => d.resolve()));

    expect(dbl.bodies).toEqual([
      expect.objectContaining({ title: 'Track 0', album: 'Album', album_artist: 'Artist' }),
    ]);
    await waitFor(() => expect(result.current.savingAll).toBe(false));
  });

  it('retries only the tracks that failed when the control is tapped again', async () => {
    const tracks = [unownedTrack(0), unownedTrack(1), unownedTrack(2)];
    const dbl = savingPort(albumSeed(tracks));
    const { result } = await renderAlbum(dbl.port, albumResult, tracks.length);

    act(() => result.current.onSaveAll());
    await waitFor(() => expect(dbl.pending).toHaveLength(tracks.length));
    await act(async () => {
      dbl.pending[0]!.resolve();
      dbl.pending[1]!.reject(new Error('save failed'));
      dbl.pending[2]!.resolve();
    });
    await waitFor(() => expect(result.current.savingAll).toBe(false));

    act(() => result.current.onSaveAll());
    await waitFor(() => expect(dbl.started).toBe(tracks.length + 1));
    await tick();

    expect(dbl.savedTitles().slice(tracks.length)).toEqual(['Track 1']);
    await act(async () => dbl.pending.splice(tracks.length).forEach((d) => d.resolve()));
  });

  it('holds a track queued behind the concurrency limit out of its own row control', async () => {
    const tracks = Array.from({ length: SAVE_ALL_CONCURRENCY + 2 }, (_, i) => unownedTrack(i));
    const dbl = savingPort(albumSeed(tracks));
    const { result } = await renderAlbum(dbl.port, albumResult, tracks.length);

    act(() => result.current.onSaveAll());
    await waitFor(() => expect(dbl.started).toBe(SAVE_ALL_CONCURRENCY));
    await tick();

    expect(dbl.started).toBe(SAVE_ALL_CONCURRENCY);
    expect(result.current.isSavingInBatch(tracks[tracks.length - 1]!)).toBe(true);

    for (let guard = 0; guard < MAX_ROUNDS && dbl.started < tracks.length; guard += 1) {
      await act(async () => dbl.pending.splice(0).forEach((d) => d.resolve()));
      await tick();
    }
    await act(async () => dbl.pending.splice(0).forEach((d) => d.resolve()));

    await waitFor(() =>
      expect(result.current.isSavingInBatch(tracks[tracks.length - 1]!)).toBe(false),
    );
    expect(dbl.started).toBe(tracks.length);
  });

  it('ignores taps while a save-all run is already in flight', async () => {
    const tracks = [unownedTrack(0), unownedTrack(1)];
    const dbl = savingPort(albumSeed(tracks));
    const { result } = await renderAlbum(dbl.port, albumResult, tracks.length);

    act(() => result.current.onSaveAll());
    await tick();
    act(() => result.current.onSaveAll());
    await tick();

    expect(dbl.savedTitles()).toEqual(['Track 0', 'Track 1']);

    await act(async () => dbl.pending.splice(0).forEach((d) => d.resolve()));
    await waitFor(() => expect(result.current.savingAll).toBe(false));
  });
});

describe('saving all with a partial library lookup', () => {
  const oneTrack: DiscoveryResult = {
    kind: 'track',
    title: 'A',
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };

  describe('onSaveAll with a partial library lookup', () => {
    it('does not re-save when ownership is unknown', async () => {
      const library = Array.from({ length: LOOKUP_LIMIT * LOOKUP_MAX_PAGES + 1 }, (_, i) =>
        libraryTrack(i),
      );
      const dbl = savingPort(albumSeed([oneTrack], { tracks: library }));
      const { result } = await renderAlbum(dbl.port, albumResult, 1);

      act(() => result.current.onSaveAll());
      await tick();

      expect(dbl.started).toBe(0);
      expect(result.current.savingAll).toBe(false);
    });

    it('does not start a batch when the library lookup failed', async () => {
      const dbl = savingPort(albumSeed([oneTrack]));
      const port: DetailPort = {
        ...dbl.port,
        getTracks: () => Promise.reject(new Error('library down')),
      };
      const { result, client } = await renderAlbum(port, albumResult, 1);

      expect(client.getQueryState(libraryKeys.lookup('Album'))?.status).toBe('error');
      act(() => result.current.onSaveAll());
      await tick();

      expect(dbl.started).toBe(0);
      expect(result.current.savingAll).toBe(false);
    });

    it('saves the unowned tracks once the lookup is complete', async () => {
      const dbl = savingPort(albumSeed([oneTrack]));
      const { result } = await renderAlbum(dbl.port, albumResult, 1);

      act(() => result.current.onSaveAll());
      await waitFor(() => expect(dbl.started).toBe(1));

      expect(dbl.savedTitles()).toEqual(['A']);
      await act(async () => dbl.pending.splice(0).forEach((d) => d.resolve()));
      await waitFor(() => expect(result.current.savingAll).toBe(false));
    });
  });
});

describe('useAlbumDetailState — onTrackPress', () => {
  it('pushes the given detail route carrying the tapped track', async () => {
    const push = jest.fn();
    jest.spyOn(require('expo-router'), 'useRouter').mockReturnValue({ push, replace: jest.fn() });

    const track = unownedTrack(0);
    const { result } = await renderAlbum(savingPort(albumSeed([])).port);

    result.current.onTrackPress(track);

    expect(push).toHaveBeenCalledTimes(1);
    const [href] = push.mock.calls[0]!;
    expect(href.pathname).toBe('/discover/detail');
    expect(readDetailHandoff(href.params.handoff)?.result.title).toBe(track.title);
  });
});
