import { renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { usePinnedStore } from '@shared/offline/pinnedStore';

import { offlineEligibility, useLibraryOffline, usePinnedStatus } from '../hooks/useLibraryOffline';

function makeTrack(over: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: asTrackId('track-1'),
    title: 'Aerodynamic',
    artist: 'Daft Punk',
    album: 'Discovery',
    duration_seconds: 212,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: 2001,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    ...over,
  } as TrackResponse;
}

beforeEach(() => {
  usePinnedStore.setState({ entries: {} });
});

describe('useLibraryOffline — statusOf reflects the pinned store', () => {
  it('reports undefined for a track with no pinned entry', () => {
    const { result } = renderHook(() => useLibraryOffline());
    expect(result.current.statusOf(asTrackId('track-1'))).toBeUndefined();
  });

  it('reports the live status once the store carries an entry', () => {
    usePinnedStore.setState({
      entries: { 'track-1': { trackId: asTrackId('track-1'), status: 'ready', uri: 'file:///a' } },
    });
    const { result } = renderHook(() => useLibraryOffline());
    expect(result.current.statusOf(asTrackId('track-1'))).toBe('ready');
  });
});

describe('useLibraryOffline — pin, unpin, pinMany and unpinMany delegate to the store', () => {
  it('pins through the store action and reflects the refusal it returns', () => {
    const pin = jest.fn().mockReturnValue('storage-full');
    usePinnedStore.setState({ pin });
    const { result } = renderHook(() => useLibraryOffline());
    expect(result.current.pin(asTrackId('track-1'))).toBe('storage-full');
    expect(pin).toHaveBeenCalledWith('track-1');
  });

  it('unpins through the store action', () => {
    const unpin = jest.fn();
    usePinnedStore.setState({ unpin });
    const { result } = renderHook(() => useLibraryOffline());
    result.current.unpin(asTrackId('track-1'));
    expect(unpin).toHaveBeenCalledWith('track-1');
  });

  it('pins many and unpins many through the store batch actions', async () => {
    const pinMany = jest.fn().mockResolvedValue({ requested: 1, failed: 0 });
    const unpinMany = jest.fn().mockResolvedValue({ requested: 1, failed: 0 });
    usePinnedStore.setState({ pinMany, unpinMany });
    const { result } = renderHook(() => useLibraryOffline());
    await result.current.pinMany([asTrackId('track-1')]);
    await result.current.unpinMany([asTrackId('track-1')]);
    expect(pinMany).toHaveBeenCalledWith(['track-1']);
    expect(unpinMany).toHaveBeenCalledWith(['track-1']);
  });
});

describe('usePinnedStatus — the narrow per-row selector', () => {
  it('reads only the given track, unaffected by another track pinned in the store', () => {
    usePinnedStore.setState({
      entries: { 'track-2': { trackId: asTrackId('track-2'), status: 'ready', uri: 'file:///a' } },
    });
    const { result } = renderHook(() => usePinnedStatus(asTrackId('track-1')));
    expect(result.current).toBeUndefined();
  });

  it('reads the given track once it is pinned', () => {
    usePinnedStore.setState({
      entries: { 'track-1': { trackId: asTrackId('track-1'), status: 'downloading' } },
    });
    const { result } = renderHook(() => usePinnedStatus(asTrackId('track-1')));
    expect(result.current).toBe('downloading');
  });
});

describe('offlineEligibility — the ready-and-all-pinned rule, in one place', () => {
  const statusOf = (entries: Record<string, 'ready' | undefined>) => (trackId: string) =>
    entries[trackId];

  it('counts only ready tracks as downloadable, excluding a pending one', () => {
    const tracks = [
      makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }),
      makeTrack({ id: asTrackId('p1'), acquisition_status: 'pending' }),
    ];
    const result = offlineEligibility(tracks, statusOf({}));
    expect(result.downloadableIds).toEqual(['r1']);
  });

  it('is not all-pinned while any downloadable track is missing a ready pin', () => {
    const tracks = [
      makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }),
      makeTrack({ id: asTrackId('r2'), acquisition_status: 'ready' }),
    ];
    const result = offlineEligibility(tracks, statusOf({ r1: 'ready' }));
    expect(result.pinnedCount).toBe(1);
    expect(result.allPinned).toBe(false);
  });

  it('is all-pinned once every downloadable track carries a ready pin', () => {
    const tracks = [
      makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }),
      makeTrack({ id: asTrackId('r2'), acquisition_status: 'ready' }),
    ];
    const result = offlineEligibility(tracks, statusOf({ r1: 'ready', r2: 'ready' }));
    expect(result.allPinned).toBe(true);
  });

  it('is not all-pinned when there is nothing downloadable', () => {
    const tracks = [makeTrack({ acquisition_status: 'pending' })];
    const result = offlineEligibility(tracks, statusOf({}));
    expect(result.allPinned).toBe(false);
  });

  it('treats a queued or downloading pin as not-pinned, distinct from ready', () => {
    const tracks = [makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' })];
    const downloading = (_trackId: string) => 'downloading' as const;
    const result = offlineEligibility(tracks, downloading);
    expect(result.pinnedCount).toBe(0);
    expect(result.allPinned).toBe(false);
  });
});

let mockOfflineDownloadsSupported = true;
jest.mock('@shared/offline/offlineSupport', () => ({
  get offlineDownloadsSupported() {
    return mockOfflineDownloadsSupported;
  },
}));

describe('useLibraryOffline — tells every library caller whether offline downloads exist here', () => {
  afterEach(() => {
    mockOfflineDownloadsSupported = true;
  });

  it('reports offline downloads unsupported on web', () => {
    mockOfflineDownloadsSupported = false;
    const { result } = renderHook(() => useLibraryOffline());
    expect(result.current.supported).toBe(false);
  });

  it('reports offline downloads supported on native', () => {
    const { result } = renderHook(() => useLibraryOffline());
    expect(result.current.supported).toBe(true);
  });
});
