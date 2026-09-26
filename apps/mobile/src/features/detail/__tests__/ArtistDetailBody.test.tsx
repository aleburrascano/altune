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

// Probe (#2816): edge cases of the section split, pinned through the body a
// caller renders. The ticket promises behaviour unchanged by the split.

let mockProbeWindowWidth: number | null = null;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => {
  const actual = jest.requireActual('react-native/Libraries/Utilities/useWindowDimensions');
  return {
    __esModule: true,
    default: () =>
      mockProbeWindowWidth === null
        ? actual.default()
        : { width: mockProbeWindowWidth, height: 800, scale: 2, fontScale: 1 },
  };
});

function replyLibraryTracks(count: number) {
  const rows = Array.from({ length: count }, (_, i) => libraryTrackRow(i, 'Boards of Canada'));
  __http.reply(TRACKS, {
    status: 200,
    json: { items: rows, total: rows.length, limit: 200, offset: 0, has_more: false },
  });
}

function libraryAlbumGroup(index: number) {
  return {
    key: `boc-album-${index}`,
    album: `Album ${index}`,
    artist: 'Boards of Canada',
    artwork_url: null,
    year: 1998 + index,
    track_count: 10,
    most_recent_added_at: '2024-01-01T00:00:00Z',
  };
}

const LISTENERS_ONLY = {
  has_content: true,
  mbid: '',
  listeners: 12345,
  playcount: 0,
  tags: [],
  bio: '',
  similar: [],
  duration: 0,
  album: '',
};

function flatStyle(node: { props: { style?: unknown } }) {
  const { StyleSheet } = require('react-native');
  return StyleSheet.flatten(node.props.style) ?? {};
}

describe('ArtistDetailBody probe: section order when a section is empty or missing', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it('keeps explore discography ahead of about when the artist has no top tracks', async () => {
    replyLibraryTracks(0);
    const { toJSON } = renderLibraryArtistBody({ lastfmError: true });

    await screen.findByTestId('detail-explore-discography');
    await screen.findByTestId('detail-lastfm-unavailable');

    const ids = collectTestIds(toJSON());
    expect(ids.some((id) => id.startsWith('detail-top-track-'))).toBe(false);
    expect(ids.indexOf('detail-explore-discography')).toBeLessThan(
      ids.indexOf('detail-lastfm-unavailable'),
    );
  });

  it('still shows top tracks before explore discography when there is no about section', async () => {
    replyLibraryTracks(2);
    const { toJSON } = renderLibraryArtistBody();

    await screen.findByTestId('detail-top-track-0');
    await screen.findByTestId('detail-explore-discography');

    const ids = collectTestIds(toJSON());
    expect(ids).not.toContain('detail-lastfm-unavailable');
    expect(ids.indexOf('detail-top-track-1')).toBeLessThan(
      ids.indexOf('detail-explore-discography'),
    );
  });
});

describe('ArtistDetailBody probe: the top-tracks cap of 5', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it('shows all 4 tracks of an artist below the cap with no Show all', async () => {
    replyLibraryTracks(4);
    renderLibraryArtistBody();

    await screen.findByTestId('detail-top-track-3');
    expect(screen.queryByTestId('detail-top-track-4')).toBeNull();
    expect(screen.queryByTestId('detail-show-all-tracks')).toBeNull();
  });

  it('shows exactly 5 tracks of an artist at the cap with no Show all', async () => {
    replyLibraryTracks(5);
    renderLibraryArtistBody();

    await screen.findByTestId('detail-top-track-4');
    expect(screen.queryByTestId('detail-top-track-5')).toBeNull();
    expect(screen.queryByTestId('detail-show-all-tracks')).toBeNull();
  });

  it('hides the 6th track of an artist one above the cap until Show all is pressed', async () => {
    replyLibraryTracks(6);
    renderLibraryArtistBody();

    await screen.findByTestId('detail-top-track-4');
    expect(screen.queryByTestId('detail-top-track-5')).toBeNull();

    fireEvent.press(screen.getByTestId('detail-show-all-tracks'));

    expect(await screen.findByTestId('detail-top-track-5')).toBeTruthy();
    expect(screen.queryByTestId('detail-top-track-6')).toBeNull();
  });
});

describe('ArtistDetailBody probe: explore discography collapses back', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    replyLibraryTracks(0);
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it('returns to the Explore label under the same testID on a second press', async () => {
    renderLibraryArtistBody();

    fireEvent.press(await screen.findByTestId('detail-explore-discography'));
    await waitFor(() =>
      expect(screen.getByTestId('detail-explore-discography').props.accessibilityLabel).toBe(
        'Collapse discography',
      ),
    );

    fireEvent.press(screen.getByTestId('detail-explore-discography'));

    await waitFor(() =>
      expect(screen.getByTestId('detail-explore-discography').props.accessibilityLabel).toBe(
        'Explore full discography',
      ),
    );
  });

  it('exposes the explore toggle as a button at least 48pt tall', async () => {
    renderLibraryArtistBody();

    const toggle = await screen.findByTestId('detail-explore-discography');
    expect(toggle.props.accessibilityRole).toBe('button');
    expect(flatStyle(toggle).minHeight).toBeGreaterThanOrEqual(48);
  });
});

describe('ArtistDetailBody probe: the facts row with each fact alone', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    __http.reset();
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it('shows only Listeners for an artist with no library tracks or releases', async () => {
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    replyLibraryTracks(0);
    renderLibraryArtistBody({ lastfm: LISTENERS_ONLY });

    const facts = await screen.findByTestId('detail-artist-facts');
    expect(within(facts).getByText(/^listeners$/i)).toBeTruthy();
    expect(within(facts).getByText('12.3K')).toBeTruthy();
    expect(within(facts).queryByText(/^in library$/i)).toBeNull();
    expect(within(facts).queryByText(/^releases$/i)).toBeNull();
  });

  it('shows only In library for an artist with library tracks and nothing else', async () => {
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    replyLibraryTracks(3);
    renderLibraryArtistBody();

    const facts = await screen.findByTestId('detail-artist-facts');
    expect(within(facts).getByText(/^in library$/i)).toBeTruthy();
    expect(within(facts).getByText('3')).toBeTruthy();
    expect(within(facts).queryByText(/^listeners$/i)).toBeNull();
    expect(within(facts).queryByText(/^releases$/i)).toBeNull();
  });

  it('shows only Releases for an artist with library albums but no tracks or listeners', async () => {
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    replyLibraryTracks(0);
    __http.reply('GET /v1/library/albums', {
      status: 200,
      json: { items: [libraryAlbumGroup(0), libraryAlbumGroup(1)], total: 2 },
    });
    renderLibraryArtistBody();

    const facts = await screen.findByTestId('detail-artist-facts');
    expect(within(facts).getByText(/^releases$/i)).toBeTruthy();
    expect(within(facts).getByText('2')).toBeTruthy();
    expect(within(facts).queryByText(/^in library$/i)).toBeNull();
    expect(within(facts).queryByText(/^listeners$/i)).toBeNull();
  });
});

describe('ArtistDetailBody probe: the wide web layout still composes the artist sections', () => {
  const { Platform } = require('react-native');
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    replyLibraryTracks(2);
  });

  afterEach(() => {
    warnSpy.mockRestore();
    Platform.OS = 'ios';
    mockProbeWindowWidth = null;
  });

  it('places top tracks, explore and about in the right column on a wide web window', async () => {
    Platform.OS = 'web';
    mockProbeWindowWidth = 1440;
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ArtistDetailBody
          chrome={{
            title: 'Boards of Canada',
            artworkUrl: 'https://cdn.altune.test/boc.jpg',
            onBack: jest.fn(),
          }}
          result={libraryArtist()}
          detailRoute="/library/detail"
          lastfmError
        />
      </QueryClientProvider>,
    );

    await screen.findByTestId('detail-top-track-0');
    expect(screen.getByTestId('detail-body-wide')).toBeTruthy();
    const right = screen.getByTestId('detail-body-right');
    const left = screen.getByTestId('detail-body-left');
    expect(within(right).getByTestId('detail-top-track-0')).toBeTruthy();
    expect(within(right).getByTestId('detail-explore-discography')).toBeTruthy();
    expect(within(right).getByTestId('detail-lastfm-unavailable')).toBeTruthy();
    expect(within(left).queryByTestId('detail-explore-discography')).toBeNull();
  });
});
