// #2852: a quick-save on an album/artist row that is already owned and `failed`
// must retry the acquisition instead of re-POSTing a create, which the server
// dedups onto the existing failed row and schedules nothing.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { asTrackId } from '@shared/api-client/ids';
import { trackIdentityKey, useTrackStatusStore } from '@shared/acquisition/trackStatusStore';

import { useOwnedPlayback, type OwnedPlaybackContext } from '../hooks/useOwnedPlayback';
import type { SaveTrack } from '../hooks/useSaveTrack';

const mockRetryAcquisition = jest.fn<Promise<void>, [unknown]>();
jest.mock('@shared/api-client/tracks', () => ({
  retryAcquisition: (trackId: unknown) => mockRetryAcquisition(trackId),
}));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({ playFromList: jest.fn(), shuffleFromList: jest.fn() }),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

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

    // Mirrors what useSaveTrack itself leaves behind when its own create POST fails:
    // the identity stays linked to the client-minted placeholder id, marked failed.
    act(() => {
      const identity = trackIdentityKey(track.title, track.subtitle ?? '')!;
      useTrackStatusStore.getState().link(identity, asTrackId('optimistic-abc'));
      useTrackStatusStore
        .getState()
        .patch(asTrackId('optimistic-abc'), { acquisitionStatus: 'failed', failureMessage: 'boom' });
    });

    act(() => {
      result.current.onQuickSave(track);
    });

    expect(save.mutate).toHaveBeenCalledTimes(1);
    expect(mockRetryAcquisition).not.toHaveBeenCalled();
  });
});
