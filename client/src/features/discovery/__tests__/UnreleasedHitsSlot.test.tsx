import { AccessibilityInfo } from 'react-native';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import type { DiscoverySearchResponse } from '@shared/apiClient/discovery';

import { resetSearchState } from '@features/discovery/domain/searchState';
import { DiscoverScreen } from '@features/discovery/ui/DiscoverScreen';
import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { warmUpFirstRender } from '@testing/warmUpFirstRender';
import {
  createPlaybackWrapper,
  createTestQueryClient,
  idlePlayback,
  signInTestUser,
} from '@testing/queryHarness';

jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

const { __http } = require('@testing/doubles/fetch.js');

const SEARCH = 'GET /v1/discovery/search';
const UNRELEASED_SEARCH = 'GET /v1/discovery/unreleased/search';
const SECTION_CAP = 5;
const HIT_COUNT = 7;
const FIRST_ID = 100;
const DEBOUNCE_WAIT_MS = 450;
const HIT_TEST_ID_PATTERN = /^discover-unreleased-hit-/;

const startQueue = jest.fn<Promise<void>, unknown[]>(() => Promise.resolve());
const PLAYBACK = { ...idlePlayback, startQueue };

function mainResponse(tracks: number): DiscoverySearchResponse {
  const items = Array.from({ length: tracks }, (_, i) =>
    resultFixture({ title: `Main ${i}`, subtitle: 'Main Artist' }),
  );
  return {
    query: 'leak title',
    query_norm: 'leak title',
    results: items,
    sections: [{ kind: 'track' as const, items, has_more: false }].filter(
      (section) => section.items.length > 0,
    ),
    providers: [{ provider: 'spotify', status: 'ok', result_count: tracks, latency_ms: 10 }],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: tracks,
    offset: 0,
    has_more: false,
  };
}

function leakHit(index: number, overrides: Record<string, unknown> = {}) {
  return {
    genius_id: FIRST_ID + index,
    title: `Leak ${index}`,
    cover_url: null,
    genius_url: 'https://genius.com/leak',
    release_date: null,
    has_media: true,
    audio_links: [],
    extras: {},
    artist: { provider: 'spotify', external_id: `artist-${index}`, name: `Artist ${index}` },
    ...overrides,
  };
}

function leakHits(count: number) {
  return Array.from({ length: count }, (_, i) => leakHit(i));
}

function serve(tracks: number, hits: unknown[]): void {
  __http.reply('POST /v1/discovery/events', { status: 204 });
  __http.reply('GET /v1/discovery/search-history', { json: { items: [] } });
  __http.reply(SEARCH, { json: mainResponse(tracks) });
  __http.reply(UNRELEASED_SEARCH, { json: { items: hits } });
}

function renderDiscover() {
  const Base = createPlaybackWrapper(createTestQueryClient({ mutations: true }), PLAYBACK);
  return render(
    <Base>
      <DiscoverScreen />
    </Base>,
  );
}

warmUpFirstRender(async () => {
  signInTestUser();
  serve(0, []);
  renderDiscover();
  typeQuery('leak title');
  await screen.findByTestId('discover-search-input');
});

function typeQuery(text: string): void {
  fireEvent.changeText(screen.getByTestId('discover-search-input'), text);
}

beforeEach(() => {
  resetSearchState();
  startQueue.mockClear();
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(true);
  signInTestUser();
});

describe('unreleased search in Discover', () => {
  it('shows an Unreleased section in All and a chip that lists every hit with its artist', async () => {
    serve(1, leakHits(HIT_COUNT));
    renderDiscover();

    typeQuery('leak title');

    const section = await screen.findByTestId('discover-unreleased-section');
    expect(section).toBeTruthy();
    expect(screen.getAllByTestId(HIT_TEST_ID_PATTERN)).toHaveLength(SECTION_CAP);
    expect(screen.getByText('Main 0')).toBeTruthy();
    expect(screen.getByText('Artist 0')).toBeTruthy();

    fireEvent.press(screen.getByTestId('discover-filter-unreleased'));

    expect(screen.queryByTestId('discover-unreleased-section')).toBeNull();
    expect(screen.getAllByTestId(HIT_TEST_ID_PATTERN)).toHaveLength(HIT_COUNT);
    expect(screen.getByText('Artist 6')).toBeTruthy();
  });

  it('still shows the hits when the main search finds nothing', async () => {
    serve(0, leakHits(1));
    renderDiscover();

    typeQuery('leak title');

    expect(await screen.findByTestId('discover-unreleased-section')).toBeTruthy();
    expect(screen.getByTestId('discover-filter-unreleased')).toBeTruthy();
  });

  it('hides the section and the chip when there are no hits', async () => {
    serve(1, []);
    renderDiscover();

    typeQuery('leak title');

    expect(await screen.findByText('Main 0')).toBeTruthy();
    await waitFor(() => expect(__http.countFor(UNRELEASED_SEARCH)).toBe(1));
    expect(screen.queryByTestId('discover-unreleased-section')).toBeNull();
    expect(screen.queryByTestId('discover-filter-unreleased')).toBeNull();
  });

  it('hides the section and the chip when the unreleased request fails, and keeps main results', async () => {
    __http.reply(UNRELEASED_SEARCH, { status: 500, json: { error: 'boom' } });
    serve(1, []);
    renderDiscover();

    typeQuery('leak title');

    expect(await screen.findByText('Main 0')).toBeTruthy();
    await waitFor(() => expect(__http.countFor(UNRELEASED_SEARCH)).toBe(1));
    expect(screen.queryByTestId('discover-unreleased-section')).toBeNull();
    expect(screen.queryByTestId('discover-filter-unreleased')).toBeNull();
  });

  it('sends the search once on the debounced query and not at all under 2 characters', async () => {
    serve(1, []);
    renderDiscover();

    typeQuery('l');
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, DEBOUNCE_WAIT_MS));
    });
    expect(__http.countFor(UNRELEASED_SEARCH)).toBe(0);

    typeQuery('le');
    typeQuery('leak title');
    await waitFor(() => expect(__http.countFor(UNRELEASED_SEARCH)).toBe(1));
    expect(new URLSearchParams(__http.last().query).get('q')).toBe('leak title');
  });

  it('plays the best version of a track the user already owns when its card is tapped', async () => {
    const owned = {
      audio_links: [
        { provider: 'soundcloud', external_id: 'sc-1', url: 'https://soundcloud.com/a' },
      ],
      extras: { owned_track_id: 'track-owned', owned_acquisition_status: 'ready' },
    };
    serve(1, [leakHit(0, owned)]);
    renderDiscover();

    typeQuery('leak title');
    fireEvent.press(await screen.findByTestId(`detail-unreleased-${FIRST_ID}`));

    await waitFor(() => expect(startQueue).toHaveBeenCalledTimes(1));
  });
});
