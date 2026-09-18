// Regression for issue #1663: the tracklist showed the same always-tappable
// Retry whether the fetch hit a one-off network blip or a failure the server
// had already settled (a `discovery.*` code). Tapping Retry on the settled one
// deterministically failed again, with nothing telling the user it would.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { AlbumDetailBody } from '../ui/AlbumDetailBody';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), back: jest.fn() }),
}));

// apiFetch demands a live session before it ever calls fetch; hand it one so the
// tracks request actually reaches the http double (and then fails there).
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

// The album is not in the library, so the tracklist is entirely the API's —
// the fetch under test.
jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracksForAlbum: () => [],
}));

// Playback wiring is irrelevant to the error affordance under test; avoid
// needing a PlaybackProvider.
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
