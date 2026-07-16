/**
 * useSaveTrack — optimistic insert on mutate, rollback on error
 * (view-result-detail slice 15). Renders the hook against a real QueryClient
 * seeded with a ['library'] cache; createTrack is mocked.
 */
 
import { act, renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { useSaveTrack } from '../hooks/useSaveTrack';
import type { CreateTrackRequest, ListTracksResponse, TrackResponse } from '../../../shared/api-client/types';

const mockCreateTrack = jest.fn();
jest.mock('../../../shared/api-client/tracks', () => ({
  createTrack: (body: unknown) => mockCreateTrack(body),
}));

// Isolate the unit from the best-effort library_add telemetry side effect.
jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: jest.fn() }),
}));

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
    failure_reason: null,
  };
}

const _BODY: CreateTrackRequest = {
  title: 'Midnight City',
  artist: 'M83',
  album: 'Hurry Up',
  duration_seconds: 244,
  artwork_url: null,
  isrc: null,
  year: null,
  genre: null,
  album_artist: null,
  track_number: null,
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

  it('does not fabricate a one-track library when library-home never loaded', async () => {
    // Regression: a user whose library-home query errored (stale-token 401)
    // saved a song and the optimistic insert invented {total: 1} over the error
    // state. Because albums/artists are grouped client-side from that array, a
    // 273-track library rendered as one song, one album, one artist — and
    // staleTime: Infinity pinned it there for the rest of the session.
    const qc = _seededClient();
    expect(qc.getQueryData(['library-home'])).toBeUndefined();
    mockCreateTrack.mockResolvedValueOnce(_existing('real'));

    const { result } = renderHook(() => useSaveTrack(), { wrapper: _wrapper(qc) });
    act(() => {
      result.current.mutate(_BODY);
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(qc.getQueryData(['library-home'])).toBeUndefined();
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
