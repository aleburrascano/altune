import { render, screen, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { detailHref } from '@shared/navigation/detailHandoff';

import { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
import { DetailPortProvider } from '@features/detail/ports/DetailPortProvider';
import { UnreleasedAllScreen } from '@features/detail/ui/unreleased/ArtistUnreleasedAll';
import {
  createPlaybackWrapper,
  createTestQueryClient,
  idlePlayback,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

type RedirectProps = { href: string };
let mockParams: { handoff?: string; where?: string } = {};
jest.mock('expo-router', () => {
  const { Text: RNText } = jest.requireActual('react-native');
  return {
    useLocalSearchParams: () => mockParams,
    Redirect: ({ href }: RedirectProps) => <RNText>{`redirect:${href}`}</RNText>,
  };
});

const UNRELEASED = 'GET /v1/discovery/artists/spotify/artist-1/unreleased';
const TRACK_COUNT = 25;
const GENIUS_ID_BASE = 9000;
const OFF_STREAMING_ID = 9101;
const ON_STREAMING_ID = 9102;
const OTHER_OFF_STREAMING_ID = 9103;
const OTHER_ON_STREAMING_ID = 9104;
const ROW_TEST_ID = /^detail-unreleased-\d+$/;

const ARTIST: DiscoveryResult = {
  kind: 'artist',
  title: 'Full List Artist',
  subtitle: null,
  image_url: null,
  confidence: 'high',
  sources: [{ provider: 'spotify', external_id: 'artist-1', url: 'https://s.example/1' }],
  extras: {},
};

function genius(index: number, offStreaming: boolean) {
  return {
    genius_id: index,
    title: `Full list entry ${index}`,
    cover_url: null,
    genius_url: `https://genius.com/full-list-entry-${index}-lyrics`,
    release_date: null,
    has_media: true,
    audio_links: [],
    off_streaming: offStreaming,
    extras: {},
  };
}

function openArtist(): void {
  mockParams = { handoff: detailHref('/discover/unreleased', ARTIST).params.handoff };
}

function renderScreen() {
  const Base = createPlaybackWrapper(createTestQueryClient({ mutations: true }), idlePlayback);
  return render(
    <Base>
      <DetailPortProvider port={httpDetailPort}>
        <UnreleasedAllScreen />
      </DetailPortProvider>
    </Base>,
  );
}

beforeEach(async () => {
  mockParams = {};
  await signInTestUser();
});

describe('UnreleasedAllScreen', () => {
  it('lists every one of an artist with 25 tracks as its own row', async () => {
    const items = Array.from({ length: TRACK_COUNT }, (_, i) => genius(GENIUS_ID_BASE + i, true));
    __http.reply(UNRELEASED, { status: 200, json: { items } });
    openArtist();
    renderScreen();

    await waitFor(() => expect(screen.getAllByTestId(ROW_TEST_ID)).toHaveLength(TRACK_COUNT));
    expect(screen.getByTestId('detail-unreleased-all-list').props.horizontal).toBeFalsy();
  });

  it('shows the rail first chip group when no where param is given', async () => {
    const items = [genius(OFF_STREAMING_ID, true), genius(ON_STREAMING_ID, false)];
    __http.reply(UNRELEASED, { status: 200, json: { items } });
    openArtist();
    renderScreen();

    expect(await screen.findByText(`Full list entry ${OFF_STREAMING_ID}`)).toBeTruthy();
    expect(screen.queryByText(`Full list entry ${ON_STREAMING_ID}`)).toBeNull();
  });

  it('shows the other group when the where param asks for it', async () => {
    const items = [genius(OTHER_OFF_STREAMING_ID, true), genius(OTHER_ON_STREAMING_ID, false)];
    __http.reply(UNRELEASED, { status: 200, json: { items } });
    openArtist();
    mockParams = { ...mockParams, where: 'unreleased' };
    renderScreen();

    expect(await screen.findByText(`Full list entry ${OTHER_ON_STREAMING_ID}`)).toBeTruthy();
    expect(screen.queryByText(`Full list entry ${OTHER_OFF_STREAMING_ID}`)).toBeNull();
  });

  it('redirects when the handoff is missing', () => {
    mockParams = { handoff: 'gone-handoff' };
    renderScreen();
    expect(screen.getByText('redirect:/discover')).toBeTruthy();
  });
});
