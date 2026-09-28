import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { AlbumDetailBody } from '../ui/AlbumDetailBody';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), back: jest.fn() }),
}));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

const mockUseLibraryTracksForAlbum = jest.fn();
jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracksForAlbum: () => mockUseLibraryTracksForAlbum(),
}));

jest.mock('../hooks/useOwnedPlayback', () => ({
  useOwnedPlayback: () => ({
    owned: { playable: [], unownedCount: 0, acquiringCount: 0 },
    playButton: { label: 'Play', disabled: true },
    onPlayOwned: jest.fn(),
    ownedFor: () => null,
    onQuickSave: jest.fn(),
  }),
}));

const ALBUM_TRACKS = 'GET /v1/discovery/albums/deezer/d1/tracks';

describe('a library album\'s "More from this album" section', () => {
  beforeEach(() => {
    mockUseLibraryTracksForAlbum.mockImplementation(() => {
      const { asTrackId } = require('@shared/api-client/ids');
      return [
        {
          id: asTrackId('trk-1'),
          title: 'The Chain',
          artist: 'Fleetwood Mac',
          album: 'Rumours',
          duration_seconds: 271,
          added_at: '2024-01-01T00:00:00Z',
          acquisition_status: 'ready',
          artwork_url: null,
          failure_reason: null,
          year: 1977,
          genre: null,
          track_number: 1,
          album_artist: 'Fleetwood Mac',
          isrc: null,
          audio_ref: null,
        },
      ];
    });
  });

  const SEARCH = 'GET /v1/discovery/search';

  function libraryAlbum(): DiscoveryResult {
    return {
      kind: 'album',
      title: 'Rumours',
      subtitle: 'Fleetwood Mac',
      image_url: null,
      confidence: 'high',
      sources: [],
      extras: {},
    };
  }

  const searchResponse = {
    status: 200,
    json: {
      query: 'rumours fleetwood mac',
      query_norm: 'rumours fleetwood mac',
      results: [
        {
          kind: 'album',
          title: 'Rumours',
          subtitle: 'Fleetwood Mac',
          image_url: null,
          confidence: 'high',
          sources: [{ provider: 'deezer', external_id: 'd1', url: 'https://deezer.example/d1' }],
          extras: {},
        },
      ],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      offset: 0,
      has_more: false,
    },
  };

  function renderBody() {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    return render(
      <QueryClientProvider client={queryClient}>
        <AlbumDetailBody
          chrome={{ title: 'Rumours', artworkUrl: null, onBack: jest.fn() }}
          result={libraryAlbum()}
          detailRoute="/library/detail"
        />
      </QueryClientProvider>,
    );
  }

  describe('AlbumDetailBody: "More from this album" when the tracks-for-album step fails', () => {
    beforeEach(() => {
      __http.replyAll({ status: 200, json: { items: [], provider_name: 'deezer', status: 'ok' } });
      __http.reply(SEARCH, searchResponse);
      __http.fail(ALBUM_TRACKS);
    });

    it('surfaces an error+retry instead of silently hiding the section', async () => {
      renderBody();

      const error = await screen.findByTestId('detail-more-from-album-error');
      expect(error).toBeTruthy();
      expect(screen.getByTestId('detail-more-from-album-retry')).toBeTruthy();
    });

    it('re-invokes the tracks-for-album request when Retry is tapped', async () => {
      renderBody();

      await waitFor(() => expect(__http.countFor(ALBUM_TRACKS)).toBe(1));

      const retry = await screen.findByTestId('detail-more-from-album-retry');
      fireEvent.press(retry);

      await waitFor(() => expect(__http.countFor(ALBUM_TRACKS)).toBe(2));
    });
  });

  describe('AlbumDetailBody: "More from this album" when discovery succeeds', () => {
    beforeEach(() => {
      __http.replyAll({ status: 200, json: { items: [], provider_name: 'deezer', status: 'ok' } });
      __http.reply(SEARCH, searchResponse);
      __http.reply(ALBUM_TRACKS, {
        status: 200,
        json: {
          items: [
            {
              kind: 'track',
              title: 'Dreams',
              subtitle: 'Fleetwood Mac',
              image_url: null,
              confidence: 'high',
              sources: [{ provider: 'deezer', external_id: 'd-dreams', url: 'https://d/dreams' }],
              extras: {},
            },
          ],
          provider_name: 'deezer',
          status: 'ok',
        },
      });
    });

    it('renders the section header and never shows the error state', async () => {
      renderBody();

      await screen.findByTestId('detail-more-from-album');
      expect(screen.queryByTestId('detail-more-from-album-error')).toBeNull();
    });
  });

  describe('AlbumDetailBody: "More from this album" when the tracks step is degraded', () => {
    it.each(['timeout', 'rate_limited', 'circuit_open', 'error'] as const)(
      'surfaces the error+retry for status %s',
      async (status) => {
        __http.replyAll({
          status: 200,
          json: { items: [], provider_name: 'deezer', status: 'ok' },
        });
        __http.reply(SEARCH, searchResponse);
        __http.reply(ALBUM_TRACKS, {
          status: 200,
          json: { items: [], provider_name: 'deezer', status },
        });

        renderBody();

        expect(await screen.findByTestId('detail-more-from-album-error')).toBeTruthy();
        expect(screen.getByTestId('detail-more-from-album-retry')).toBeTruthy();
      },
    );
  });

  describe('AlbumDetailBody: "More from this album" when the source-search step fails', () => {
    let warnSpy: jest.SpyInstance;

    beforeEach(() => {
      warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
      __http.replyAll({ status: 200, json: { items: [], provider_name: 'deezer', status: 'ok' } });
      __http.fail(SEARCH);
    });

    afterEach(() => {
      warnSpy.mockRestore();
    });

    it('surfaces an error+retry instead of rendering nothing', async () => {
      renderBody();

      expect(await screen.findByTestId('detail-more-from-album-error')).toBeTruthy();
      expect(screen.getByTestId('detail-more-from-album-retry')).toBeTruthy();
    });

    it('re-invokes the search request when Retry is tapped', async () => {
      renderBody();

      await waitFor(() => expect(__http.countFor(SEARCH)).toBe(1));

      fireEvent.press(await screen.findByTestId('detail-more-from-album-retry'));

      await waitFor(() => expect(__http.countFor(SEARCH)).toBe(2));
    });
  });

  describe('AlbumDetailBody: "More from this album" when the tracks step is settled as unserved', () => {
    it('shows the error without a retry', async () => {
      __http.replyAll({ status: 200, json: { items: [], provider_name: 'deezer', status: 'ok' } });
      __http.reply(SEARCH, searchResponse);
      __http.reply(ALBUM_TRACKS, { status: 404, json: { code: 'discovery.content_unserved' } });

      renderBody();

      expect(await screen.findByTestId('detail-more-from-album-settled')).toBeTruthy();
      expect(screen.queryByTestId('detail-more-from-album-retry')).toBeNull();
    });
  });
});

describe('the tracklist error', () => {
  beforeEach(() => {
    mockUseLibraryTracksForAlbum.mockImplementation(() => []);
  });

  function sourcedAlbum(): DiscoveryResult {
    return {
      kind: 'album',
      title: 'Rumours',
      subtitle: 'Fleetwood Mac',
      image_url: null,
      confidence: 'high',
      sources: [{ provider: 'deezer', external_id: 'd1', url: 'https://deezer.example/d1' }],
      extras: {},
    };
  }

  function renderBody() {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    return render(
      <QueryClientProvider client={queryClient}>
        <AlbumDetailBody
          chrome={{ title: 'Rumours', artworkUrl: null, onBack: jest.fn() }}
          result={sourcedAlbum()}
          detailRoute="/library/detail"
        />
      </QueryClientProvider>,
    );
  }

  beforeEach(() => {
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
  });

  describe('AlbumDetailBody: the tracklist error tells a settled failure from a transient one', () => {
    it('drops the retry and explains when the content is settled as unserved', async () => {
      __http.reply(ALBUM_TRACKS, {
        status: 404,
        json: { code: 'discovery.content_unserved', detail: 'no provider serves this album' },
      });

      renderBody();

      expect(await screen.findByTestId('detail-tracklist-error')).toBeTruthy();
      expect(screen.getByTestId('detail-tracklist-settled')).toBeTruthy();
      expect(screen.queryByTestId('detail-tracklist-retry')).toBeNull();
    });

    it('keeps the retry when the fetch failed in transit', async () => {
      __http.fail(ALBUM_TRACKS);

      renderBody();

      expect(await screen.findByTestId('detail-tracklist-error')).toBeTruthy();
      expect(screen.getByTestId('detail-tracklist-retry')).toBeTruthy();
      expect(screen.queryByTestId('detail-tracklist-settled')).toBeNull();
    });
  });
});

describe('AlbumDetailBody: the Save N pill', () => {
  const { within } = require('@testing-library/react-native');

  function sourcedAlbum(): DiscoveryResult {
    return {
      kind: 'album',
      title: 'Rumours',
      subtitle: 'Fleetwood Mac',
      image_url: null,
      confidence: 'high',
      sources: [{ provider: 'deezer', external_id: 'd1', url: 'https://deezer.example/d1' }],
      extras: {},
    };
  }

  const ALBUM_TRACKS_ITEMS = {
    status: 200,
    json: {
      items: [
        {
          kind: 'track',
          title: 'Dreams',
          subtitle: 'Fleetwood Mac',
          image_url: null,
          confidence: 'high',
          sources: [{ provider: 'deezer', external_id: 'd-dreams', url: 'https://d/dreams' }],
          extras: {},
        },
      ],
      total: 1,
    },
  };

  function saveResponse() {
    return {
      status: 201,
      json: {
        id: 'server-1',
        title: 'Dreams',
        artist: 'Fleetwood Mac',
        album: 'Rumours',
        duration_seconds: 257,
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
      },
    };
  }

  function renderBody() {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    return render(
      <QueryClientProvider client={queryClient}>
        <AlbumDetailBody
          chrome={{ title: 'Rumours', artworkUrl: null, onBack: jest.fn() }}
          result={sourcedAlbum()}
          detailRoute="/library/detail"
        />
      </QueryClientProvider>,
    );
  }

  function mockUnownedCount(unownedCount: number) {
    const ownedPlayback = require('../hooks/useOwnedPlayback');
    jest.spyOn(ownedPlayback, 'useOwnedPlayback').mockReturnValue({
      owned: { playable: [], unownedCount, acquiringCount: 0 },
      playButton: { label: 'Play', disabled: true },
      onPlayOwned: jest.fn(),
      ownedFor: () => null,
      onQuickSave: jest.fn(),
    });
  }

  beforeEach(() => {
    mockUseLibraryTracksForAlbum.mockImplementation(() => []);
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    __http.reply(ALBUM_TRACKS, {
      status: 200,
      json: { ...ALBUM_TRACKS_ITEMS.json, provider_name: 'deezer', status: 'ok' },
    });
    __http.reply(ALBUM_TRACKS, ALBUM_TRACKS_ITEMS);
    __http.reply('POST /v1/tracks', saveResponse());
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('hides the pill when nothing is unowned', async () => {
    renderBody();

    await screen.findByTestId('detail-tracklist');
    expect(screen.queryByTestId('detail-save-all')).toBeNull();
  });

  it('labels the pill and announces it for unowned tracks', async () => {
    mockUnownedCount(3);
    renderBody();

    await screen.findByTestId('detail-tracklist');
    const pill = screen.getByTestId('detail-save-all');

    expect(screen.getByLabelText('Save 3 tracks to your library')).toBeTruthy();
    expect(within(pill).getByText('Save 3')).toBeTruthy();
    expect(pill.props.accessibilityState?.disabled).toBe(false);
  });

  it('disables the pill and shows "Saving…" once a save-all run starts', async () => {
    mockUnownedCount(1);
    renderBody();

    await screen.findByTestId('detail-tracklist');
    const pill = screen.getByTestId('detail-save-all');
    fireEvent.press(pill);

    expect(within(pill).getByText('Saving…')).toBeTruthy();
    expect(pill.props.accessibilityState?.disabled).toBe(true);
  });
});

describe('AlbumDetailBody: the facts row', () => {
  beforeEach(() => {
    __http.reply(ALBUM_TRACKS, {
      status: 200,
      json: {
        items: [
          {
            kind: 'track',
            title: 'Dreams',
            subtitle: 'Fleetwood Mac',
            image_url: null,
            confidence: 'high',
            sources: [{ provider: 'deezer', external_id: 'd-dreams', url: 'https://d/dreams' }],
            extras: { duration_seconds: 257 },
          },
        ],
        provider_name: 'deezer',
        status: 'ok',
      },
    });
  });

  const { within } = require('@testing-library/react-native');

  function sourcedAlbum(): DiscoveryResult {
    return {
      kind: 'album',
      title: 'Rumours',
      subtitle: 'Fleetwood Mac',
      image_url: null,
      confidence: 'high',
      sources: [{ provider: 'deezer', external_id: 'd1', url: 'https://deezer.example/d1' }],
      extras: {},
    };
  }

  beforeEach(() => {
    mockUseLibraryTracksForAlbum.mockImplementation(() => []);
    __http.replyAll({ status: 200, json: { items: [], total: 0 } });
    __http.reply(ALBUM_TRACKS, {
      status: 200,
      json: {
        items: [
          {
            kind: 'track',
            title: 'Dreams',
            subtitle: 'Fleetwood Mac',
            image_url: null,
            confidence: 'high',
            sources: [{ provider: 'deezer', external_id: 'd-dreams', url: 'https://d/dreams' }],
            extras: { duration_seconds: 257 },
          },
        ],
        total: 1,
      },
    });
  });

  it('shows track count, runtime and the released year from mbYear', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <AlbumDetailBody
          chrome={{ title: 'Rumours', artworkUrl: null, onBack: jest.fn() }}
          result={sourcedAlbum()}
          detailRoute="/library/detail"
          mbYear={1977}
        />
      </QueryClientProvider>,
    );

    await screen.findByTestId('detail-tracklist');
    const facts = await screen.findByTestId('detail-album-meta');
    expect(within(facts).getByText('1')).toBeTruthy();
    expect(within(facts).getByText('4 min')).toBeTruthy();
    expect(within(facts).getByText('1977')).toBeTruthy();
  });
});
