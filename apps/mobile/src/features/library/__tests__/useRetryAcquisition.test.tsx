/**
 * useRetryAcquisition optimistically flips a failed row back to pending across
 * the caches, so retry is realtime instead of waiting for a refetch.
 */

import { renderHook, act } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement } from 'react';
import type { ReactNode } from 'react';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';

import { useRetryAcquisition } from '../hooks/useRetryAcquisition';

jest.mock('@shared/api-client/tracks', () => ({
  retryAcquisition: jest.fn(async () => undefined),
}));

function failedTrack(): TrackResponse {
  return {
    id: 't1',
    title: 'Outro',
    artist: 'M83',
    album: null,
    duration_seconds: null,
    added_at: '2026-06-30T00:00:00Z',
    acquisition_status: 'failed',
    artwork_url: null,
    failure_reason: 'no source found',
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

function wrapperFor(qc: QueryClient) {
  return ({ children }: { children: ReactNode }): ReactNode =>
    createElement(QueryClientProvider, { client: qc }, children);
}

describe('useRetryAcquisition', () => {
  it('optimistically sets the track to pending and clears the failure reason', () => {
    const qc = new QueryClient();
    qc.setQueryData<ListTracksResponse>(['library-home'], {
      items: [failedTrack()],
      total: 1,
      limit: 50,
      offset: 0,
      has_more: false,
    });

    const { result } = renderHook(() => useRetryAcquisition(), { wrapper: wrapperFor(qc) });

    act(() => {
      result.current.mutate('t1');
    });

    const data = qc.getQueryData<ListTracksResponse>(['library-home']);
    expect(data?.items[0]).toMatchObject({ acquisition_status: 'pending', failure_reason: null });
  });
});
