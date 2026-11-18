import React from 'react';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react-native';

import type { LastFmEnrichmentResponse } from '@shared/apiClient';
import type { DiscoveryResult } from '@shared/catalog';
import { ArtistDetailBody } from '@features/detail';
import { warmUpFirstRender } from '@testing/warmUpFirstRender';
import { readDetailHandoff } from '@shared/navigation';
import {
  createRecordingPorts,
  createTestQueryClient,
  createPlaybackWrapper,
  idlePlayback,
  signInTestUser,
  type RecordingFeaturePorts,
} from '@testing/queryHarness';
import { setPlatformOS } from '@testing/setPlatformOS';
import { WINDOW_WIDTH, setWindowWidth } from '@testing/setWindowWidth';
import {
  navigatorWrapper,
  recordingNavigator,
  type RecordingNavigator,
} from '@features/detail/__tests__/recordingNavigator';
export type ArtistDetailBodyLastfmFixture = LastFmEnrichmentResponse;

const { __http } = require('@testing/doubles/fetch.js');

let navigator: RecordingNavigator;

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

function bodyWrapper(ports: RecordingFeaturePorts = createRecordingPorts()) {
  return navigatorWrapper(
    navigator,
    createPlaybackWrapper(createTestQueryClient({ mutations: true }), idlePlayback, ports),
  );
}

function renderBody(ports?: RecordingFeaturePorts) {
  return render(
    <ArtistDetailBody
      chrome={{ title: 'Boards of Canada', artworkUrl: null, onBack: jest.fn() }}
      result={libraryArtist()}
      detailRoute="/library/detail"
    />,
    { wrapper: bodyWrapper(ports) },
  );
}

beforeEach(signInTestUser);
beforeEach(() => {
  navigator = recordingNavigator();
});
beforeEach(() => setWindowWidth(WINDOW_WIDTH.phone));

warmUpFirstRender(async () => {
  await signInTestUser();
  __http.replyAll({ status: 200, json: { items: [], total: 0 } });
  renderBody();
  await screen.findByTestId('detail-explore-discography');
});

describe('ArtistDetailBody: explore-discography Retry after a failed search step', () => {
  beforeEach(() => {
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    __http.fail(SEARCH);
  });

  it('reports the failed search through the detail port', async () => {
    const ports = createRecordingPorts();
    renderBody(ports);

    fireEvent.press(screen.getByTestId('detail-explore-discography'));

    await screen.findByTestId('detail-explore-error');
    expect(ports.detail.warnings).toContainEqual([
      '[detail] discovery search failed',
      expect.objectContaining({ kind: 'artist', title: 'Boards of Canada', artist: null }),
    ]);
  });

  it('re-invokes the discovery search query when Retry is tapped (was a no-op)', async () => {
    renderBody();

    fireEvent.press(screen.getByTestId('detail-explore-discography'));

    await waitFor(() => expect(__http.countFor(SEARCH)).toBe(1));

    const retry = await screen.findByTestId('detail-explore-retry');
    expect(screen.getByTestId('detail-explore-error')).toBeTruthy();

    fireEvent.press(retry);

    await waitFor(() => expect(__http.countFor(SEARCH)).toBe(2));
  });
});

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

function renderLibraryArtistBody(
  options: {
    lastfm?: DiscoveryResult extends never
      ? never
      : Parameters<typeof ArtistDetailBody>[0]['lastfm'];
    lastfmError?: boolean;
  } = {},
) {
  return render(
    <ArtistDetailBody
      chrome={{ title: 'Boards of Canada', artworkUrl: null, onBack: jest.fn() }}
      result={libraryArtist()}
      detailRoute="/library/detail"
      lastfm={options.lastfm}
      lastfmError={options.lastfmError}
    />,
    { wrapper: bodyWrapper() },
  );
}

describe('ArtistDetailBody: section composition, top-tracks cap, explore toggle and facts', () => {
  beforeEach(() => {
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    const rows = Array.from({ length: 7 }, (_, i) => libraryTrackRow(i, 'Boards of Canada'));
    __http.reply(TRACKS, {
      status: 200,
      json: { items: rows, total: rows.length, limit: 200, offset: 0, has_more: false },
    });
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
  beforeEach(() => {
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
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
  beforeEach(() => {
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
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
  beforeEach(() => {
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    replyLibraryTracks(0);
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
  beforeEach(() => {
    __http.reset();
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
  beforeEach(() => {
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    replyLibraryTracks(2);
  });

  afterEach(() => {
    setPlatformOS('ios');
  });

  it('places top tracks, explore and about in the right column on a wide web window', async () => {
    setPlatformOS('web');
    setWindowWidth(WINDOW_WIDTH.desktop);
    render(
      <ArtistDetailBody
        chrome={{
          title: 'Boards of Canada',
          artworkUrl: 'https://cdn.altune.test/boc.jpg',
          onBack: jest.fn(),
        }}
        result={libraryArtist()}
        detailRoute="/library/detail"
        lastfmError
      />,
      { wrapper: bodyWrapper() },
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

describe('ArtistDetailBody: top track and album taps pin the exact router push', () => {
  beforeEach(() => {
    __http.reset();
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    replyLibraryTracks(1);
    __http.reply('GET /v1/library/albums', {
      status: 200,
      json: { items: [libraryAlbumGroup(0)], total: 1 },
    });
  });

  it('pushes the exact top track tapped', async () => {
    const { push } = navigator;
    renderLibraryArtistBody();

    fireEvent.press(await screen.findByTestId('detail-top-track-0'));

    expect(push).toHaveBeenCalledTimes(1);
    const [href] = push.mock.calls[0]!;
    expect(href.pathname).toBe('/library/detail');
    expect(readDetailHandoff(href.params.handoff)?.result.title).toBe('Song 0');
  });

  it('pushes the exact album tapped', async () => {
    const { push } = navigator;
    renderLibraryArtistBody();

    fireEvent.press(await screen.findByTestId('detail-album-0'));

    expect(push).toHaveBeenCalledTimes(1);
    const [href] = push.mock.calls[0]!;
    expect(href.pathname).toBe('/library/detail');
    expect(readDetailHandoff(href.params.handoff)?.result.title).toBe('Album 0');
  });
});

describe('ArtistDetailBody: incomplete library track lookup', () => {
  beforeEach(() => {
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
  });

  it('warns that the library list may be partial when the lookup has more pages than fetched', async () => {
    const rows = [libraryTrackRow(0, 'Boards of Canada')];
    __http.reply(TRACKS, {
      status: 200,
      json: { items: rows, total: 1000, limit: 200, offset: 0, has_more: true },
    });
    renderLibraryArtistBody();

    expect(await screen.findByTestId('detail-library-tracks-incomplete')).toBeTruthy();
  });

  it('shows no warning when the lookup is complete', async () => {
    const rows = [libraryTrackRow(0, 'Boards of Canada')];
    __http.reply(TRACKS, {
      status: 200,
      json: { items: rows, total: 1, limit: 200, offset: 0, has_more: false },
    });
    renderLibraryArtistBody();

    await screen.findByTestId('detail-top-track-0');
    expect(screen.queryByTestId('detail-library-tracks-incomplete')).toBeNull();
  });
});

type RecordedRequest = { path: string; query: string };

describe('ArtistDetailBody: the artist content request', () => {
  const CONTENT = 'GET /v1/discovery/artists/deezer/art-1/content';

  it('asks for the artist by its ids, never by its name', async () => {
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    __http.reply(CONTENT, {
      status: 200,
      json: {
        top_tracks: { items: [], provider_name: 'deezer', status: 'ok' },
        albums: { items: [], provider_name: 'deezer', status: 'ok' },
      },
    });
    render(
      <ArtistDetailBody
        chrome={{ title: 'Daft Punk', artworkUrl: null, onBack: jest.fn() }}
        result={{
          ...libraryArtist(),
          title: 'Daft Punk',
          sources: [{ provider: 'deezer', external_id: 'art-1', url: '' }],
        }}
        detailRoute="/discover/detail"
      />,
      { wrapper: bodyWrapper() },
    );

    await waitFor(() => expect(__http.countFor(CONTENT)).toBe(1));
    const request = __http.requests.find((r: RecordedRequest) => r.path.endsWith('/art-1/content'));
    expect(new URLSearchParams(request.query).has('name')).toBe(false);
    expect(new URLSearchParams(request.query).get('ids')).toBe('deezer:art-1');
  });
});
