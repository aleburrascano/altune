import { act, renderHook } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useAlbumDetailState } from '../hooks/useAlbumDetailState';
import { SAVE_ALL_CONCURRENCY } from '../save-all';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

const mockUseSaveTrack = jest.fn();
const mockUseAlbumTracks = jest.fn();

jest.mock('../hooks/useSaveTrack', () => ({
  useSaveTrack: () => mockUseSaveTrack(),
}));
jest.mock('../hooks/useAlbumTracks', () => ({
  useAlbumTracks: () => mockUseAlbumTracks(),
}));
jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracksForAlbum: () => [],
}));
jest.mock('../hooks/useAlbumDiscovery', () => ({
  useAlbumDiscovery: () => ({ tracks: [], isLoading: false, isError: false, refetch: jest.fn() }),
}));
jest.mock('../hooks/useOwnedPlayback', () => ({
  useOwnedPlayback: () => ({
    owned: { playable: [], unownedCount: mockUnownedCount, acquiringCount: 0 },
    playButton: { label: 'Play', disabled: true },
    onPlayOwned: jest.fn(),
    saveStateFor: jest.fn(),
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

function unownedTrack(i: number): DiscoveryResult {
  return {
    kind: 'track',
    title: `Track ${i}`,
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
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
  const mutateAsync = jest.fn(() => {
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

beforeEach(() => {
  mockUnownedCount = 0;
  mockUseSaveTrack.mockReset();
  mockUseAlbumTracks.mockReset();
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

  it('ignores taps while a save-all run is already in flight', async () => {
    const dbl = saveDouble();
    mockUseSaveTrack.mockReturnValue(dbl.save);
    const tracks = [unownedTrack(0), unownedTrack(1)];
    mockUnownedCount = 2;
    mockUseAlbumTracks.mockReturnValue({
      tracks,
      isLoading: false,
      isError: false,
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
