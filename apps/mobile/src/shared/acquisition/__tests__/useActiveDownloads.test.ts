import { renderHook, act } from '@testing-library/react-native';

import { useDownloadStore } from '@shared/acquisition/downloadStore';

import { useActiveDownloads } from '../useActiveDownloads';

beforeEach(() => {
  useDownloadStore.getState().reset();
});

afterEach(() => {
  useDownloadStore.getState().reset();
});

describe('useActiveDownloads', () => {
  it('passes through the entries the underlying store hook produces, sorted and shaped the same', () => {
    act(() => {
      useDownloadStore.getState().start('t2', { title: 'Second Track', artist: 'Artist B' });
      useDownloadStore.getState().start('t1', { title: 'First Track', artist: 'Artist A' });
    });

    const { result } = renderHook(() => useActiveDownloads());

    expect(result.current).toEqual([
      {
        trackId: 't1',
        phase: 'finding',
        title: 'First Track',
        artist: 'Artist A',
        artworkUrl: null,
      },
      {
        trackId: 't2',
        phase: 'finding',
        title: 'Second Track',
        artist: 'Artist B',
        artworkUrl: null,
      },
    ]);
  });

  it('reflects a live store update in its return value without a remount', () => {
    const { result } = renderHook(() => useActiveDownloads());

    expect(result.current).toEqual([]);

    act(() => {
      useDownloadStore.getState().start('t9', { title: 'Live Track', artist: 'Artist C' });
    });

    expect(result.current).toEqual([
      {
        trackId: 't9',
        phase: 'finding',
        title: 'Live Track',
        artist: 'Artist C',
        artworkUrl: null,
      },
    ]);
  });
});
