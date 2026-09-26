import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { ApiError } from '@shared/errors';

import { ownedTrack } from '../hooks/useOwnedTrack';
import { useTrackSave } from '../hooks/useTrackSave';

const mockCreateTrack = jest.fn<Promise<TrackResponse>, [unknown]>();
const mockRetryAcquisition = jest.fn<Promise<void>, [unknown]>();

jest.mock('@shared/api-client/tracks', () => ({
  createTrack: (body: unknown) => mockCreateTrack(body),
  retryAcquisition: (trackId: unknown) => mockRetryAcquisition(trackId),
}));
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function track(subtitle: string | null = 'Radiohead'): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Idioteque',
    subtitle,
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

beforeEach(() => {
  mockCreateTrack.mockReset();
  mockRetryAcquisition.mockReset();
  useTrackStatusStore.getState().reset();
});

describe('useTrackSave state derivation', () => {
  it('is disabled with no failure when the track has no known artist', () => {
    const { result } = renderHook(() => useTrackSave(track(null), null), { wrapper });

    expect(result.current.state).toBe('disabled');
    expect(result.current.failure).toBeNull();
  });

  it('is add for an unowned, unattempted track', () => {
    const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

    expect(result.current.state).toBe('add');
  });

  it('is ready for a track already in the library', () => {
    const owned = ownedTrack(asTrackId('server-1'), 'ready', null);
    const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

    expect(result.current.state).toBe('ready');
  });

  it('is saving while an owned acquisition is pending', () => {
    const owned = ownedTrack(asTrackId('server-1'), 'pending', null);
    const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

    expect(result.current.state).toBe('saving');
  });

  it('is failed, with the remembered reason, for a track the server previously failed', () => {
    const owned = ownedTrack(asTrackId('server-1'), 'failed', 'network error');
    const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

    expect(result.current.state).toBe('failed');
    expect(result.current.failure).toEqual({ message: 'network error', isRetryable: true });
  });

  it('is saving once onSave dispatches a create for an unowned track', async () => {
    mockCreateTrack.mockReturnValue(new Promise(() => undefined));
    const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

    act(() => {
      result.current.onSave();
    });

    await waitFor(() => expect(result.current.state).toBe('saving'));
  });

  it('is failed for a retryable create error, saving from add', async () => {
    mockCreateTrack.mockRejectedValue(new ApiError(503, '503 unavailable'));
    const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

    act(() => {
      result.current.onSave();
    });

    await waitFor(() => expect(result.current.state).toBe('failed'));
    expect(result.current.failure).toEqual({
      message: '503 unavailable',
      isRetryable: true,
      trackId: expect.anything(),
    });
  });

  it('is rejected, with no retry offered, for a permanent create refusal', async () => {
    mockCreateTrack.mockRejectedValue(new ApiError(400, '400 duplicate_track'));
    const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

    act(() => {
      result.current.onSave();
    });

    await waitFor(() => expect(result.current.state).toBe('rejected'));
  });
});

describe('useTrackSave onSave', () => {
  it('does not call the retry or create endpoints when the state is disabled', () => {
    const { result } = renderHook(() => useTrackSave(track(null), null), { wrapper });

    act(() => {
      result.current.onSave();
    });

    expect(mockCreateTrack).not.toHaveBeenCalled();
    expect(mockRetryAcquisition).not.toHaveBeenCalled();
  });

  it('does not re-dispatch a create for a permanently rejected track', async () => {
    mockCreateTrack.mockRejectedValue(new ApiError(400, '400 duplicate_track'));
    const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

    act(() => {
      result.current.onSave();
    });
    await waitFor(() => expect(result.current.state).toBe('rejected'));
    mockCreateTrack.mockClear();

    act(() => {
      result.current.onSave();
    });

    expect(mockCreateTrack).not.toHaveBeenCalled();
  });

  it('hits the retry endpoint with the owned trackId, never a create, for a failed owned track', async () => {
    mockRetryAcquisition.mockResolvedValue(undefined);
    const trackId = asTrackId('server-1');
    const owned = ownedTrack(trackId, 'failed', 'network error');
    const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

    act(() => {
      result.current.onSave();
    });

    await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalledWith(trackId));
    expect(mockCreateTrack).not.toHaveBeenCalled();
  });
});

function savedTrack(): TrackResponse {
  return {
    id: 'srv-idioteque',
    title: 'Idioteque',
    artist: 'Radiohead',
    album: null,
    duration_seconds: null,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'pending',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  } as TrackResponse;
}

describe('useTrackSave repeated saves', () => {
  it('dispatches one create when onSave is called again while the first is saving', async () => {
    mockCreateTrack.mockReturnValue(new Promise(() => undefined));
    const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

    act(() => {
      result.current.onSave();
    });
    await waitFor(() => expect(result.current.state).toBe('saving'));
    act(() => {
      result.current.onSave();
    });

    expect(mockCreateTrack).toHaveBeenCalledTimes(1);
  });

  it('dispatches a second create on retry after a retryable failure, and clears the failure', async () => {
    mockCreateTrack.mockRejectedValueOnce(new ApiError(503, '503 unavailable'));
    mockCreateTrack.mockResolvedValueOnce(savedTrack());
    const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

    act(() => {
      result.current.onSave();
    });
    await waitFor(() => expect(result.current.state).toBe('failed'));
    act(() => {
      result.current.onSave();
    });

    await waitFor(() => expect(mockCreateTrack).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.failure).toBeNull());
    expect(result.current.state).not.toBe('failed');
  });

  it('never dispatches when a track with no known artist is saved repeatedly', () => {
    const { result } = renderHook(() => useTrackSave(track(''), null), { wrapper });

    act(() => {
      result.current.onSave();
      result.current.onSave();
    });

    expect(result.current.state).toBe('disabled');
    expect(mockCreateTrack).not.toHaveBeenCalled();
  });
});
