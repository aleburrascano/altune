import { act, renderHook } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useAlbumDetailState } from '../hooks/useAlbumDetailState';
import { SAVE_ALL_CONCURRENCY } from '../save-all';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

const mockUseSaveTrack = jest.fn();
const mockUseAlbumTracks = jest.fn();
const mockUseLibraryTracksForAlbum = jest.fn();

jest.mock('../hooks/useSaveTrack', () => ({
  useSaveTrack: () => mockUseSaveTrack(),
}));
jest.mock('../hooks/useAlbumTracks', () => ({
  useAlbumTracks: () => mockUseAlbumTracks(),
}));
jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracksForAlbum: () => mockUseLibraryTracksForAlbum(),
}));
jest.mock('../hooks/useAlbumDiscovery', () => ({
  useAlbumDiscovery: () => ({
    tracks: [],
    isLoading: false,
    isError: false,
    failure: null,
    refetch: jest.fn(),
  }),
}));
jest.mock('../hooks/useOwnedPlayback', () => ({
  useOwnedPlayback: () => ({
    owned: { playable: [], unownedCount: mockUnownedCount, acquiringCount: 0 },
    playButton: { label: 'Play', disabled: true },
    onPlayOwned: jest.fn(),
    ownedFor: () => null,
    onQuickSave: jest.fn(),
  }),
}));

let mockUnownedCount = 0;

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

type Deferred = { resolve: () => void; reject: (e: unknown) => void };

// A useSaveTrack double whose mutateAsync stays pending until the test releases
// it, so the number of concurrent saves in flight is observable. mutate is the
// old fire-and-forget path — kept so the test stays honest if the code regresses.
function saveDouble() {
  let active = 0;
  let maxConcurrent = 0;
  let started = 0;
  const pending: Deferred[] = [];
  const begin = (): void => {
    started += 1;
    active += 1;
    maxConcurrent = Math.max(maxConcurrent, active);
  };
  const mutateAsync = jest.fn((_body: { title: string }) => {
    begin();
    return new Promise<void>((resolve, reject) => {
      pending.push({
        resolve: () => {
          active -= 1;
          resolve();
        },
        reject: (e) => {
          active -= 1;
          reject(e);
        },
      });
    });
  });
  const mutate = jest.fn(() => begin());
  return {
    save: { mutate, mutateAsync, isPending: false },
    pending,
    get started() {
      return started;
    },
    get maxConcurrent() {
      return maxConcurrent;
    },
  };
}

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function savedTitles(dbl: ReturnType<typeof saveDouble>): string[] {
  return dbl.save.mutateAsync.mock.calls.map(([body]) => body.title);
}

beforeEach(() => {
  mockUnownedCount = 0;
  mockUseSaveTrack.mockReset();
  mockUseAlbumTracks.mockReset();
  mockUseLibraryTracksForAlbum.mockReset().mockImplementation(() => []);
});

describe('useAlbumDetailState — onSaveAll', () => {
  it('bounds concurrent save requests instead of firing one per unowned track at once', async () => {
    const dbl = saveDouble();
    mockUseSaveTrack.mockReturnValue(dbl.save);
    const tracks = Array.from({ length: 20 }, (_, i) => unownedTrack(i));
    mockUnownedCount = 20;
    mockUseAlbumTracks.mockReturnValue({
      tracks,
      isLoading: false,
      isError: false,
      failure: null,
      refetch: jest.fn(),
    });

    const { result } = renderHook(() => useAlbumDetailState(albumResult, '/discover/detail'));

    await act(async () => {
      result.current.onSaveAll();
      for (let guard = 0; guard < 50; guard += 1) {
        await flush();
        if (dbl.started === 20 && dbl.pending.length === 0) break;
        dbl.pending.splice(0).forEach((d) => d.resolve());
      }
    });

    expect(dbl.started).toBe(20);
    expect(dbl.maxConcurrent).toBe(SAVE_ALL_CONCURRENCY);
  });

  it('recovers the control after a partial failure rather than wedging on Saving… forever', async () => {
    const dbl = saveDouble();
    mockUseSaveTrack.mockReturnValue(dbl.save);
    const tracks = [unownedTrack(0), unownedTrack(1), unownedTrack(2)];
    mockUnownedCount = 3;
    mockUseAlbumTracks.mockReturnValue({
      tracks,
      isLoading: false,
      isError: false,
      failure: null,
      refetch: jest.fn(),
    });

    const { result } = renderHook(() => useAlbumDetailState(albumResult, '/discover/detail'));

    await act(async () => {
      result.current.onSaveAll();
      await flush();
    });

    // While saves are in flight the control reports itself busy.
    expect(result.current.savingAll).toBe(true);

    await act(async () => {
      dbl.pending[1]!.reject(new Error('save failed'));
      dbl.pending[0]!.resolve();
      dbl.pending[2]!.resolve();
      await flush();
    });

    // Once every save has settled — including the failure — the control frees up
    // again, and the failed track is still unowned and therefore retryable.
    expect(result.current.savingAll).toBe(false);
    expect(result.current.owned.unownedCount).toBe(3);
  });

  it('saves a track tagged with an empty album under the containing album instead', async () => {
    const dbl = saveDouble();
    mockUseSaveTrack.mockReturnValue(dbl.save);
    mockUnownedCount = 1;
    mockUseAlbumTracks.mockReturnValue({
      tracks: [unownedTrack(0, { album: '', album_artist: '' })],
      isLoading: false,
      isError: false,
      refetch: jest.fn(),
    });

    const { result } = renderHook(() => useAlbumDetailState(albumResult, '/discover/detail'));

    await act(async () => {
      result.current.onSaveAll();
      await flush();
      dbl.pending.splice(0).forEach((d) => d.resolve());
      await flush();
    });

    expect(dbl.save.mutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({ album: 'Album', album_artist: 'Artist' }),
    );
  });

  it('retries only the tracks that failed when the control is tapped again', async () => {
    const dbl = saveDouble();
    mockUseSaveTrack.mockReturnValue(dbl.save);
    const tracks = [unownedTrack(0), unownedTrack(1), unownedTrack(2)];
    mockUnownedCount = 3;
    mockUseAlbumTracks.mockReturnValue({
      tracks,
      isLoading: false,
      isError: false,
      failure: null,
      refetch: jest.fn(),
    });
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);

    const { result } = renderHook(() => useAlbumDetailState(albumResult, '/discover/detail'));

    await act(async () => {
      result.current.onSaveAll();
      await flush();
      dbl.pending[0]!.resolve();
      dbl.pending[1]!.reject(new Error('save failed'));
      dbl.pending[2]!.resolve();
      await flush();
    });

    await act(async () => {
      result.current.onSaveAll();
      await flush();
    });

    // The two that landed are not written a second time; only the failure is.
    expect(savedTitles(dbl).slice(3)).toEqual(['Track 1']);
    warn.mockRestore();
  });

  it('holds a track queued behind the concurrency limit out of its own row control', async () => {
    const dbl = saveDouble();
    mockUseSaveTrack.mockReturnValue(dbl.save);
    const tracks = Array.from({ length: SAVE_ALL_CONCURRENCY + 2 }, (_, i) => unownedTrack(i));
    mockUnownedCount = tracks.length;
    mockUseAlbumTracks.mockReturnValue({
      tracks,
      isLoading: false,
      isError: false,
      failure: null,
      refetch: jest.fn(),
    });

    const { result } = renderHook(() => useAlbumDetailState(albumResult, '/discover/detail'));

    await act(async () => {
      result.current.onSaveAll();
      await flush();
    });

    // The last track has not been dispatched yet — the batch still owns it, so its
    // row must not offer a quick-save that would race the dispatch.
    expect(dbl.started).toBe(SAVE_ALL_CONCURRENCY);
    expect(result.current.isSavingInBatch(tracks[tracks.length - 1]!)).toBe(true);

    await act(async () => {
      for (let guard = 0; guard < 50 && dbl.pending.length > 0; guard += 1) {
        dbl.pending.splice(0).forEach((d) => d.resolve());
        await flush();
      }
    });

    // And it is the batch's claim, not the track, that disables it: the claim lifts
    // as soon as the run is over.
    expect(result.current.isSavingInBatch(tracks[tracks.length - 1]!)).toBe(false);
  });

  it('ignores taps while a save-all run is already in flight', async () => {
    const dbl = saveDouble();
    mockUseSaveTrack.mockReturnValue(dbl.save);
    const tracks = [unownedTrack(0), unownedTrack(1)];
    mockUnownedCount = 2;
    mockUseAlbumTracks.mockReturnValue({
      tracks,
      isLoading: false,
      isError: false,
      failure: null,
      refetch: jest.fn(),
    });

    const { result } = renderHook(() => useAlbumDetailState(albumResult, '/discover/detail'));

    await act(async () => {
      result.current.onSaveAll();
      await flush();
      result.current.onSaveAll();
      await flush();
    });

    expect(dbl.save.mutateAsync).toHaveBeenCalledTimes(2);

    await act(async () => {
      dbl.pending.splice(0).forEach((d) => d.resolve());
      await flush();
    });
  });
});

describe('saving all with a partial library lookup', () => {
  const mockSave = {
    mutate: jest.fn(),
    mutateAsync: jest.fn(() => Promise.resolve()),
    isPending: false,
  };

  let mockComplete = false;

  const oneTrack = {
    kind: 'track',
    title: 'A',
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };

  const album: DiscoveryResult = {
    kind: 'album',
    title: 'Album',
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'deezer', external_id: 'a', url: 'https://deezer/a' }],
    extras: {},
  };

  beforeEach(() => {
    mockUseSaveTrack.mockReturnValue(mockSave);
    mockUseAlbumTracks.mockImplementation(() => ({
      tracks: [oneTrack],
      isLoading: false,
      isError: false,
      failure: null,
      refetch: jest.fn(),
    }));
    mockUseLibraryTracksForAlbum.mockImplementation(() =>
      Object.assign([], { complete: mockComplete }),
    );
    mockUnownedCount = 1;
  });

  beforeEach(() => mockSave.mutateAsync.mockClear());

  describe('onSaveAll with a partial library lookup', () => {
    it('does not re-save when ownership is unknown', () => {
      mockComplete = false;
      const hook = renderHook(() => useAlbumDetailState(album, '/discover/detail'));
      act(() => hook.result.current.onSaveAll());
      expect(mockSave.mutateAsync).not.toHaveBeenCalled();
    });

    it('saves the unowned tracks once the lookup is complete', () => {
      mockComplete = true;
      const hook = renderHook(() => useAlbumDetailState(album, '/discover/detail'));
      act(() => hook.result.current.onSaveAll());
      expect(mockSave.mutateAsync).toHaveBeenCalledTimes(1);
    });
  });
});
