// Regression for issue #683: when a library-only artist's discography *search*
// step fails, the "Explore Discography" Retry button used to call only
// refetchAlbums() — a query that is disabled while the search has produced no
// source, so the tap was a permanent no-op. The button must re-invoke the
// search step itself.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { ArtistDetailBody } from '../ui/ArtistDetailBody';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), back: jest.fn() }),
}));

// Playback needs a PlaybackProvider we don't set up; it is irrelevant to the
// discovery-search retry under test.
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({ playFromList: jest.fn(), enqueue: jest.fn() }),
}));

// apiFetch demands a live session before it ever calls fetch; hand it one so
// the search request actually reaches the http double (and then fails there).
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

const SEARCH = 'GET /v1/discovery/search';

function libraryArtist(): DiscoveryResult {
  return {
    kind: 'artist',
    title: 'Boards of Canada',
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

function renderBody() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ArtistDetailBody
        chrome={{ title: 'Boards of Canada', artworkUrl: null, onBack: jest.fn() }}
        result={libraryArtist()}
        detailRoute="/library/detail"
      />
    </QueryClientProvider>,
  );
}

describe('ArtistDetailBody: explore-discography Retry after a failed search step', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    // #1660: the failed search names the artist it was looking for in a log;
    // failure-logging.test.tsx asserts that line, so here it is only silenced.
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    // Every peripheral library lookup resolves empty; only the search fails.
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    __http.fail(SEARCH);
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it('re-invokes the discovery search query when Retry is tapped (was a no-op)', async () => {
    renderBody();

    // Expand the explore section so the search step runs and then fails.
    fireEvent.press(screen.getByTestId('detail-explore-discography'));

    await waitFor(() => expect(__http.countFor(SEARCH)).toBe(1));

    const retry = await screen.findByTestId('detail-explore-retry');
    // #667: the explore error state carries the same testID pair as its siblings.
    expect(screen.getByTestId('detail-explore-error')).toBeTruthy();

    fireEvent.press(retry);

    // With the bug, Retry called only refetchAlbums() on a disabled query and
    // the search count stayed at 1 forever. The fix must re-run the search.
    await waitFor(() => expect(__http.countFor(SEARCH)).toBe(2));
  });
});
