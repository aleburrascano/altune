import { act, renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { asTrackId } from '@shared/api-client/ids';
import { trackIdentityKey, useTrackStatusStore } from '@shared/acquisition/trackStatusStore';

import { useOwnedPlayback, type OwnedPlaybackContext } from '../hooks/useOwnedPlayback';
import { useSaveTrack, type SaveTrack } from '../hooks/useSaveTrack';
import { supabase } from '@shared/auth/supabaseClient';
import { createTestQueryClient, createWrapper, mockSupabaseSession } from './support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

const mockRetryAcquisition = jest.fn<Promise<void>, [unknown]>();
jest.mock('@shared/api-client/tracks', () => ({
  ...jest.requireActual('@shared/api-client/tracks'),
  retryAcquisition: (trackId: unknown) => mockRetryAcquisition(trackId),
}));
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({ playFromList: jest.fn(), shuffleFromList: jest.fn() }),
}));

let wrapper: ReturnType<typeof createWrapper>;

beforeEach(() => {
  wrapper = createWrapper(createTestQueryClient({ mutations: true }));
});

const context: OwnedPlaybackContext = {
  title: null,
  image: null,
  enrich: (track) => track,
  retryEntryPoint: 'album_row',
};

function saveDouble(): SaveTrack {
  return { mutate: jest.fn(), mutateAsync: jest.fn(), isPending: false, failure: null };
}

function trackResult(extras: Record<string, unknown> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Rollacoasta',
    subtitle: 'Grip',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras,
  };
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue(mockSupabaseSession());
  mockRetryAcquisition.mockReset();
  useTrackStatusStore.getState().reset();
});

describe('useOwnedPlayback — onQuickSave', () => {
  it('saves a never-owned track through a create, not a retry', () => {
    const save = saveDouble();
    const { result } = renderHook(() => useOwnedPlayback([], context, save), { wrapper });

    act(() => {
      result.current.onQuickSave(trackResult());
    });

    expect(save.mutate).toHaveBeenCalledTimes(1);
    expect(mockRetryAcquisition).not.toHaveBeenCalled();
  });

  it('retries an already-owned, failed row through the acquisition endpoint', async () => {
    mockRetryAcquisition.mockResolvedValue(undefined);
    const save = saveDouble();
    const trackId = asTrackId('owned-1');
    const { result } = renderHook(() => useOwnedPlayback([], context, save), { wrapper });

    await act(async () => {
      result.current.onQuickSave(
        trackResult({ owned_track_id: trackId, owned_acquisition_status: 'failed' }),
      );
    });

    expect(mockRetryAcquisition).toHaveBeenCalledWith(trackId);
    expect(save.mutate).not.toHaveBeenCalled();
  });

  it('falls back to a create when the row only carries a failed save that never reached the server', () => {
    const save = saveDouble();
    const track = trackResult();
    const { result } = renderHook(() => useOwnedPlayback([], context, save), { wrapper });

    act(() => {
      const identity = trackIdentityKey(track.title, track.subtitle ?? '')!;
      useTrackStatusStore.getState().link(identity, asTrackId('optimistic-abc'));
      useTrackStatusStore.getState().patch(asTrackId('optimistic-abc'), {
        acquisitionStatus: 'failed',
        failureMessage: 'boom',
      });
    });

    act(() => {
      result.current.onQuickSave(track);
    });

    expect(save.mutate).toHaveBeenCalledTimes(1);
    expect(mockRetryAcquisition).not.toHaveBeenCalled();
  });
});

describe('useOwnedPlayback — onQuickSave re-entrancy', () => {
  const failedId = asTrackId('failed-1');
  const failedExtras = { owned_track_id: failedId, owned_acquisition_status: 'failed' };

  function renderWithRealSave() {
    return renderHook(() => useOwnedPlayback([], context, useSaveTrack()), { wrapper });
  }

  it('sends one create when a never-owned track is tapped twice in one act', async () => {
    __http.reply('POST /v1/tracks', { status: 500 });
    const { result } = renderWithRealSave();

    await act(async () => {
      result.current.onQuickSave(trackResult());
      result.current.onQuickSave(trackResult());
    });

    await waitFor(() => expect(__http.countFor('POST /v1/tracks')).toBeGreaterThan(0));
    expect(__http.countFor('POST /v1/tracks')).toBe(1);
  });

  it('sends one retry and no create when a failed track is tapped twice in one act', async () => {
    mockRetryAcquisition.mockReturnValue(new Promise(() => {}));
    const { result } = renderWithRealSave();

    await act(async () => {
      result.current.onQuickSave(trackResult(failedExtras));
      result.current.onQuickSave(trackResult(failedExtras));
    });

    expect(mockRetryAcquisition).toHaveBeenCalledTimes(1);
    expect(__http.countFor('POST /v1/tracks')).toBe(0);
  });

  it('sends one create when the enrich fills a null subtitle and the track is tapped twice', async () => {
    __http.reply('POST /v1/tracks', { status: 500 });
    const artistPage: OwnedPlaybackContext = {
      ...context,
      enrich: (track) => ({ ...track, subtitle: track.subtitle ?? 'Grip' }),
    };
    const { result } = renderHook(() => useOwnedPlayback([], artistPage, useSaveTrack()), {
      wrapper,
    });
    const track = { ...trackResult(), subtitle: null };

    await act(async () => {
      result.current.onQuickSave(track);
      result.current.onQuickSave(track);
    });

    await waitFor(() => expect(__http.countFor('POST /v1/tracks')).toBeGreaterThan(0));
    expect(__http.countFor('POST /v1/tracks')).toBe(1);
  });

  it('retries again after a failed retry settles', async () => {
    mockRetryAcquisition.mockRejectedValue(new Error('nope'));
    const { result } = renderWithRealSave();

    await act(async () => {
      result.current.onQuickSave(trackResult(failedExtras));
    });
    await act(async () => {
      result.current.onQuickSave(trackResult(failedExtras));
    });

    expect(mockRetryAcquisition).toHaveBeenCalledTimes(2);
  });

  it('retries the first failed track again after a second failed track superseded it', async () => {
    mockRetryAcquisition.mockRejectedValue(new Error('nope'));
    const { result } = renderWithRealSave();
    const other = {
      ...trackResult({ owned_track_id: asTrackId('failed-2'), owned_acquisition_status: 'failed' }),
      title: 'Other',
    };

    await act(async () => {
      result.current.onQuickSave(trackResult(failedExtras));
      result.current.onQuickSave(other);
    });
    await act(async () => {
      result.current.onQuickSave(trackResult(failedExtras));
    });

    expect(mockRetryAcquisition).toHaveBeenCalledTimes(3);
  });
});
