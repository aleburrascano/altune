import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { clearDetailHandoffs, detailHref } from '@shared/navigation/detailHandoff';

import { DetailScreen } from '@/features/detail/ui/DetailScreen';
import {
  createTestQueryClient,
  createPlaybackWrapper,
  idlePlayback,
  signInTestUser,
} from '@/features/detail/__tests__/support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

let mockParams: { handoff?: string } = {};
let mockCanGoBack = true;
const mockBack = jest.fn();
const mockReplace = jest.fn();
jest.mock('expo-router', () => {
  const { Text: RNText } = jest.requireActual('react-native');
  return {
    useLocalSearchParams: () => mockParams,
    useRouter: () => ({
      push: jest.fn(),
      replace: mockReplace,
      back: mockBack,
      canGoBack: () => mockCanGoBack,
    }),
    useSegments: () => ['(tabs)', 'discover', 'detail'],
    Redirect: ({ href }: { href: string }) => <RNText>{`redirect:${href}`}</RNText>,
  };
});

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

function renderScreen(playback = idlePlayback) {
  render(<DetailScreen />, {
    wrapper: createPlaybackWrapper(createTestQueryClient({ mutations: true }), playback),
  });
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
  clearDetailHandoffs();
  mockParams = {};
  mockCanGoBack = true;
  mockBack.mockClear();
  mockReplace.mockClear();
  await signInTestUser();
  __http.replyAll({ status: 200, json: { items: [], total: 0 } });
  jest.spyOn(console, 'warn').mockImplementation(() => {});
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
      await new Promise((resolve) => setTimeout(resolve, SETTLE_MS));
    });
  }

  function openArtist(): void {
    mockParams = { handoff: detailHref('/discover/detail', libraryArtist).params.handoff };
    renderScreen();
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

describe('DetailScreen reads the handoff named by its route param', () => {
  it('renders the result and search id of its own navigation after a later tap', async () => {
    const previewExtras = { preview_url: 'https://p.example/first.mp3' };
    const first = detailHref('/discover/detail', track('First', previewExtras), 'search-1');
    detailHref('/discover/detail', track('Second'), 'search-2');
    const play = jest.fn().mockResolvedValue(undefined);

    mockParams = { handoff: first.params.handoff };
    renderScreen({ ...idlePlayback, play });
    fireEvent.press(await screen.findByTestId('detail-preview'));

    expect(play).toHaveBeenCalledWith(
      expect.objectContaining({ title: 'First', searchId: 'search-1' }),
    );
  });

  it('redirects to discover when the param names no handoff', () => {
    mockParams = { handoff: 'unknown' };
    renderScreen();

    expect(screen.getByText('redirect:/discover')).toBeTruthy();
  });
});

describe('DetailScreen back button', () => {
  async function pressBack(canGoBack: boolean): Promise<void> {
    mockCanGoBack = canGoBack;
    mockParams = { handoff: detailHref('/discover/detail', track('First')).params.handoff };
    renderScreen();
    fireEvent.press(await screen.findByTestId('detail-back'));
  }

  it('calls router.back() when canGoBack is true', async () => {
    await pressBack(true);

    expect(mockBack).toHaveBeenCalledTimes(1);
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it("calls router.replace('/discover') when canGoBack is false", async () => {
    await pressBack(false);

    expect(mockReplace).toHaveBeenCalledWith('/discover');
    expect(mockBack).not.toHaveBeenCalled();
  });
});
