import { act, renderHook } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { useAlbumSaveAll } from '../hooks/useAlbumSaveAll';
import { SAVE_ALL_CONCURRENCY } from '../save-all';

const album: DiscoveryResult = {
  kind: 'album',
  title: 'Album',
  subtitle: 'Artist',
  image_url: null,
  confidence: 'high',
  sources: [],
  extras: {},
};

function track(i: number): DiscoveryResult {
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

function saveDouble() {
  let active = 0;
  let maxConcurrent = 0;
  const pending: (() => void)[] = [];
  const mutateAsync = jest.fn(() => {
    active += 1;
    maxConcurrent = Math.max(maxConcurrent, active);
    return new Promise<void>((resolve) => {
      pending.push(() => {
        active -= 1;
        resolve();
      });
    });
  });
  return {
    save: { mutate: jest.fn(), mutateAsync, isPending: false } as never,
    mutateAsync,
    pending,
    get maxConcurrent() {
      return maxConcurrent;
    },
  };
}

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

async function settleAll(dbl: ReturnType<typeof saveDouble>): Promise<void> {
  for (let guard = 0; guard < 50; guard += 1) {
    await flush();
    if (dbl.pending.length === 0) return;
    dbl.pending.splice(0).forEach((resolve) => resolve());
  }
}

describe('useAlbumSaveAll', () => {
  it('bounds concurrent saves to SAVE_ALL_CONCURRENCY', async () => {
    const dbl = saveDouble();
    const candidates = Array.from({ length: 12 }, (_, i) => track(i));
    const { result } = renderHook(() =>
      useAlbumSaveAll({ album, candidates, libraryComplete: true, save: dbl.save }),
    );

    await act(async () => {
      result.current.onSaveAll();
      await settleAll(dbl);
    });

    expect(dbl.mutateAsync).toHaveBeenCalledTimes(12);
    expect(dbl.maxConcurrent).toBe(SAVE_ALL_CONCURRENCY);
  });

  it('ignores a second onSaveAll while a batch is in flight', async () => {
    const dbl = saveDouble();
    const candidates = [track(0), track(1)];
    const { result } = renderHook(() =>
      useAlbumSaveAll({ album, candidates, libraryComplete: true, save: dbl.save }),
    );

    await act(async () => {
      result.current.onSaveAll();
      result.current.onSaveAll();
      await flush();
    });

    expect(dbl.mutateAsync).toHaveBeenCalledTimes(2);
    expect(result.current.savingAll).toBe(true);
    expect(result.current.isSavingInBatch(track(0))).toBe(true);

    await act(async () => {
      await settleAll(dbl);
    });

    expect(result.current.savingAll).toBe(false);
    expect(result.current.isSavingInBatch(track(0))).toBe(false);
  });

  it('does nothing until the library is complete', async () => {
    const dbl = saveDouble();
    const { result } = renderHook(() =>
      useAlbumSaveAll({
        album,
        candidates: [track(0)],
        libraryComplete: false,
        save: dbl.save,
      }),
    );

    await act(async () => {
      result.current.onSaveAll();
      await flush();
    });

    expect(dbl.mutateAsync).not.toHaveBeenCalled();
  });
});

describe('useAlbumSaveAll across sign-out', () => {
  it('stops issuing saves for queued tracks once the session ends', async () => {
    const dbl = saveDouble();
    const { result } = renderHook(() =>
      useAlbumSaveAll({
        album,
        candidates: Array.from({ length: 8 }, (_, i) => track(i)),
        libraryComplete: true,
        save: dbl.save,
      }),
    );

    await act(async () => {
      result.current.onSaveAll();
      await flush();
    });
    expect(dbl.mutateAsync).toHaveBeenCalledTimes(SAVE_ALL_CONCURRENCY);

    await act(async () => {
      runSignOutCleanups();
      await settleAll(dbl);
    });

    expect(dbl.mutateAsync).toHaveBeenCalledTimes(SAVE_ALL_CONCURRENCY);
  });
});
