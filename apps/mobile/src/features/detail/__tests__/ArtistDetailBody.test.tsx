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
import { within } from '@testing-library/react-native';
import type { LastFmEnrichmentResponse } from '@shared/api-client/enrichment';
export type ArtistDetailBodyLastfmFixture = LastFmEnrichmentResponse;

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

// #2816: split ArtistDetailBody into section components with one shared
// collapsible header. These pin the composition's observable behaviour
// (section order, the top-tracks cap, explore expand/collapse, facts) before
// any structural edit, and again after — this same test file must pass
// unmodified through the split.

function collectTestIds(node: unknown, out: string[] = []): string[] {
  if (node == null) return out;
  if (Array.isArray(node)) {
    for (const child of node) collectTestIds(child, out);
    return out;
  }
  const element = node as { props?: { testID?: string }; children?: unknown };
  if (element.props?.testID) out.push(element.props.testID);
  if (element.children !== undefined) collectTestIds(element.children, out);
  return out;
}

function libraryTrackRow(index: number, artist: string) {
  return {
    id: `lib-track-${index}`,
    title: `Song ${index}`,
    artist,
    album: 'An Album',
    duration_seconds: 200,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: artist,
    isrc: null,
    audio_ref: null,
  };
}

const TRACK_CAP = 5;
const TRACKS = 'GET /v1/tracks';

function renderLibraryArtistBody(options: {
  lastfm?: DiscoveryResult extends never ? never : Parameters<typeof ArtistDetailBody>[0]['lastfm'];
  lastfmError?: boolean;
} = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ArtistDetailBody
        chrome={{ title: 'Boards of Canada', artworkUrl: null, onBack: jest.fn() }}
        result={libraryArtist()}
        detailRoute="/library/detail"
        lastfm={options.lastfm}
        lastfmError={options.lastfmError}
      />
    </QueryClientProvider>,
  );
}

describe('ArtistDetailBody: section composition, top-tracks cap, explore toggle and facts', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    const rows = Array.from({ length: 7 }, (_, i) => libraryTrackRow(i, 'Boards of Canada'));
    __http.reply(TRACKS, {
      status: 200,
      json: { items: rows, total: rows.length, limit: 200, offset: 0, has_more: false },
    });
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it('orders the sections: top tracks, then explore discography, then about', async () => {
    const { toJSON } = renderLibraryArtistBody({ lastfmError: true });

    await screen.findByTestId('detail-top-track-0');
    await screen.findByTestId('detail-explore-discography');
    await screen.findByTestId('detail-lastfm-unavailable');

    const ids = collectTestIds(toJSON());
    const topTracksIndex = ids.indexOf('detail-top-track-0');
    const exploreIndex = ids.indexOf('detail-explore-discography');
    const aboutIndex = ids.indexOf('detail-lastfm-unavailable');

    expect(topTracksIndex).toBeGreaterThanOrEqual(0);
    expect(topTracksIndex).toBeLessThan(exploreIndex);
    expect(exploreIndex).toBeLessThan(aboutIndex);
  });

  it('caps a library artist at 5 top tracks and reveals the rest via Show all', async () => {
    renderLibraryArtistBody();

    await screen.findByTestId('detail-top-track-0');
    for (let i = 0; i < TRACK_CAP; i += 1) {
      expect(screen.getByTestId(`detail-top-track-${i}`)).toBeTruthy();
    }
    expect(screen.queryByTestId(`detail-top-track-${TRACK_CAP}`)).toBeNull();

    fireEvent.press(screen.getByTestId('detail-show-all-tracks'));

    expect(await screen.findByTestId('detail-top-track-6')).toBeTruthy();
  });

  it('toggles explore discography a11y label and testID on press', async () => {
    renderLibraryArtistBody();

    const toggle = await screen.findByTestId('detail-explore-discography');
    expect(toggle.props.accessibilityLabel).toBe('Explore full discography');

    fireEvent.press(toggle);

    await waitFor(() =>
      expect(screen.getByTestId('detail-explore-discography').props.accessibilityLabel).toBe(
        'Collapse discography',
      ),
    );
  });

  it('shows the Listeners fact built from the lastfm response', async () => {
    renderLibraryArtistBody({
      lastfm: {
        has_content: true,
        mbid: '',
        listeners: 12345,
        playcount: 0,
        tags: [],
        bio: '',
        similar: [],
        duration: 0,
        album: '',
      },
    });

    const facts = await screen.findByTestId('detail-artist-facts');
    expect(within(facts).getByText('12.3K')).toBeTruthy();
  });

  it('hides the facts row when there is nothing to show', async () => {
    // The describe block's beforeEach registers a standing TRACKS rule with
    // 7 ready tracks, and the http double matches rules in registration
    // order, so a later __http.reply(TRACKS, ...) here would never be
    // reached. Reset the double and rebuild only what a truly empty artist
    // needs: buildArtistFacts shows "In library" from owned.playable +
    // acquiringCount and "Releases" from apiAlbums/libraryAlbums, so this
    // fixture must have zero library tracks, zero albums and no lastfm
    // listeners (the default here, since no `lastfm` option is passed).
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    __http.reply(TRACKS, {
      status: 200,
      json: { items: [], total: 0, limit: 200, offset: 0, has_more: false },
    });

    renderLibraryArtistBody();

    await screen.findByTestId('detail-explore-discography');
    expect(screen.queryByTestId('detail-artist-facts')).toBeNull();
  });
});
