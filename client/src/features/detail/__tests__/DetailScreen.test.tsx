import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { DiscoveryResult } from '@shared/catalog';
import type { Navigator } from '@shared/navigation';
import type { PlaybackContextValue } from '@shared/playback';
import { runSignOutCleanups } from '@shared/session';

import { DetailScreen } from '@features/detail';
import {
  createRecordingPorts,
  createTestQueryClient,
  createPlaybackWrapper,
  idlePlayback,
  signInTestUser,
} from '@testing/queryHarness';

import { handoffFor } from '@features/detail/__tests__/screenHandoff';
import { recordingNavigator } from '@features/detail/__tests__/recordingNavigator';

const { __http } = require('@testing/doubles/fetch.js');

const SETTLE_MS = 50;

interface RecordedRequest {
  path: string;
  query: string;
}

const SEARCH = 'GET /v1/discovery/search';

function track(title: string, extras: DiscoveryResult['extras'] = {}): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras,
  };
}

interface ScreenOptions {
  playback?: PlaybackContextValue;
  navigator?: Navigator;
  searchId?: string;
}

function renderScreen(
  result: DiscoveryResult,
  { playback = idlePlayback, navigator = recordingNavigator(), searchId }: ScreenOptions = {},
) {
  const Base = createPlaybackWrapper(
    createTestQueryClient({ mutations: true }),
    playback,
    createRecordingPorts(),
  );
  return render(
    <Base>
      <DetailScreen
        handoff={handoffFor(result, '/discover/detail', searchId)}
        tabRoot="discover"
        navigator={navigator}
      />
    </Base>,
  );
}

function searchResponse(results: DiscoveryResult[]) {
  return {
    status: 200,
    json: {
      query: 'Radiohead',
      query_norm: 'radiohead',
      results,
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: results.length,
      offset: 0,
      has_more: false,
    },
  };
}

beforeEach(async () => {
  jest.useFakeTimers();
  runSignOutCleanups();
  await signInTestUser();
  __http.replyAll({ status: 200, json: { items: [], total: 0 } });
});

afterEach(() => {
  jest.useRealTimers();
});

describe('DetailScreen on a library artist page', () => {
  const libraryArtist: DiscoveryResult = { ...track('Radiohead'), kind: 'artist', subtitle: null };
  const found = {
    ...libraryArtist,
    sources: [{ provider: 'deezer', external_id: '399', url: 'https://d/399' }],
  };

  function photoSearches(): number {
    return __http.requests.filter(
      (request: RecordedRequest) =>
        request.path === '/v1/discovery/search' &&
        new URLSearchParams(request.query).get('limit') === '1',
    ).length;
  }

  async function settle(): Promise<void> {
    await act(async () => {
      await jest.advanceTimersByTimeAsync(SETTLE_MS);
    });
  }

  function openArtist(): void {
    renderScreen(libraryArtist);
  }

  it('does not search again for a photo once the first search found the artist', async () => {
    __http.reply(SEARCH, searchResponse([found]));
    openArtist();

    await waitFor(() => expect(__http.countFor(SEARCH)).toBe(1));
    await settle();

    expect(photoSearches()).toBe(0);
  });

  it('does not search again while the first search is still running', async () => {
    __http.hang(SEARCH);
    openArtist();

    await waitFor(() => expect(__http.countFor(SEARCH)).toBe(1));
    await settle();

    expect(photoSearches()).toBe(0);
  });

  it('searches for a photo when the first search found nothing', async () => {
    __http.reply(SEARCH, searchResponse([]));
    openArtist();

    await waitFor(() => expect(photoSearches()).toBe(1));
  });
});

describe('DetailScreen renders the handoff it is given', () => {
  it('plays the result and search id its handoff carries when the preview is pressed', async () => {
    const previewExtras = { preview_url: 'https://p.example/first.mp3' };
    const first = track('First', previewExtras);
    const play = jest.fn().mockResolvedValue(undefined);

    renderScreen(first, { playback: { ...idlePlayback, play }, searchId: 'search-1' });
    fireEvent.press(await screen.findByTestId('detail-preview'));

    expect(play).toHaveBeenCalledWith(
      expect.objectContaining({ title: 'First', searchId: 'search-1' }),
    );
  });
});

describe('DetailScreen back button', () => {
  async function pressBack(canGoBack: boolean) {
    const navigator = { ...recordingNavigator(), canGoBack: () => canGoBack };
    renderScreen(track('First'), { navigator });
    fireEvent.press(await screen.findByTestId('detail-back'));
    return navigator;
  }

  it('goes back when there is a screen behind it', async () => {
    const navigator = await pressBack(true);

    expect(navigator.back).toHaveBeenCalledTimes(1);
    expect(navigator.replace).not.toHaveBeenCalled();
  });

  it('replaces itself with the tab root when it was opened directly', async () => {
    const navigator = await pressBack(false);

    expect(navigator.replace).toHaveBeenCalledWith('/discover');
    expect(navigator.back).not.toHaveBeenCalled();
  });
});
