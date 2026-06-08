/**
 * useSaveTrack — optimistic insert on mutate, rollback on error
 * (view-result-detail slice 15). Renders the hook against a real QueryClient
 * seeded with a ['library'] cache; createTrack is mocked.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { act, renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

const mockCreateTrack = jest.fn();
jest.mock('../../../shared/api-client/tracks', () => ({
  createTrack: (body: unknown) => mockCreateTrack(body),
}));

import { useSaveTrack } from '../hooks/useSaveTrack';
import type { CreateTrackRequest, ListTracksResponse, TrackResponse } from '../../../shared/api-client/types';

function _existing(id: string): TrackResponse {
  return {
    id,
    title: 'Existing',
    artist: 'A',
    album: null,
    duration_seconds: null,
    added_at: '2026-05-01T12:00:00Z',
    acquisition_status: 'pending',
    artwork_url: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

const _BODY: CreateTrackRequest = {
  title: 'Midnight City',
  artist: 'M83',
  album: 'Hurry Up',
  duration_seconds: 244,
  artwork_url: null,
};

function _seededClient(): QueryClient {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const data: InfiniteData<ListTracksResponse> = {
    pageParams: [0],
    pages: [{ items: [_existing('a')], total: 1, limit: 50, offset: 0, has_more: false }],
  };
  qc.setQueryData(['library'], data);
  return qc;
}

function _wrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

afterEach(() => {
  mockCreateTrack.mockReset();
});

function _libraryIds(qc: QueryClient): string[] {
  const data = qc.getQueryData<InfiniteData<ListTracksResponse>>(['library']);
  return data?.pages.flatMap((p) => p.items.map((t) => t.id)) ?? [];
}

describe('useSaveTrack', () => {
  it('optimistically inserts the track at the head of the library', async () => {
    const qc = _seededClient();
    let resolve!: (t: TrackResponse) => void;
    mockCreateTrack.mockReturnValueOnce(new Promise<TrackResponse>((r) => (resolve = r)));

    const { result } = renderHook(() => useSaveTrack(), { wrapper: _wrapper(qc) });
    act(() => {
      result.current.mutate(_BODY);
    });

    await waitFor(() => expect(_libraryIds(qc)).toHaveLength(2));
    expect(_libraryIds(qc)[0]).toContain('optimistic:');

    // settle so no act() warning leaks
    act(() => resolve(_existing('a')));
    await waitFor(() => expect(result.current.isPending).toBe(false));
  });

  it('rolls back the optimistic insert when the save fails', async () => {
    const qc = _seededClient();
    mockCreateTrack.mockRejectedValueOnce(new Error('boom'));

    const { result } = renderHook(() => useSaveTrack(), { wrapper: _wrapper(qc) });
    act(() => {
      result.current.mutate(_BODY);
    });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(_libraryIds(qc)).toEqual(['a']);
  });
});
