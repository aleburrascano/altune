import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { DiscoveryResult } from '@shared/catalog';

import { httpDetailPort, DetailPortProvider, UnreleasedAllScreen } from '@features/detail';
import {
  createPlaybackWrapper,
  createTestQueryClient,
  idlePlayback,
  signInTestUser,
} from '@testing/queryHarness';

import { handoffFor } from '@features/detail/__tests__/screenHandoff';
import { recordingNavigator } from '@features/detail/__tests__/recordingNavigator';

const { __http } = require('@testing/doubles/fetch.js');

const UNRELEASED = 'GET /v1/discovery/artists/spotify/artist-1/unreleased';
const EVENTS = 'POST /v1/discovery/events';
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

const LINK = { provider: 'soundcloud', external_id: 'sc-1', url: 'https://sc.example/1' };
const NO_LINKS: (typeof LINK)[] = [];
const ENTRY_TITLE = /^Full list entry/;
const LOW_ID = 9201;
const HIGH_ID = 9202;
const NO_LINK_ID = 9203;
const LOW_RANK = 1;
const HIGH_RANK = 5;
const PENDING_ID = 9301;

function ranked(index: number, rank: number, isPlayable: boolean) {
  const audio_links = isPlayable ? [LINK] : NO_LINKS;
  return { ...genius(index, true), quality_rank: rank, audio_links };
}

function shownTitles(): string[] {
  return screen.getAllByText(ENTRY_TITLE).map((node) => node.props.children as string);
}

function recordedActions(): unknown[] {
  return (__http.requests as { path: string; body: string }[])
    .filter((request) => request.path === '/v1/discovery/events')
    .map((request) => JSON.parse(request.body))
    .filter((event) => event.type === 'user_action')
    .map((event) => event.payload.action);
}

function pick(testID: string): void {
  fireEvent.press(screen.getByTestId(testID));
}

function renderScreen(where?: string) {
  const Base = createPlaybackWrapper(createTestQueryClient({ mutations: true }), idlePlayback);
  return render(
    <Base>
      <DetailPortProvider port={httpDetailPort}>
        <UnreleasedAllScreen
          handoff={handoffFor(ARTIST, '/discover/unreleased')}
          navigator={recordingNavigator()}
          where={where}
        />
      </DetailPortProvider>
    </Base>,
  );
}

beforeEach(async () => {
  await signInTestUser();
});

describe('UnreleasedAllScreen', () => {
  it('gives the list every one of an artist with 25 tracks', async () => {
    const items = Array.from({ length: TRACK_COUNT }, (_, i) => genius(GENIUS_ID_BASE + i, true));
    __http.reply(UNRELEASED, { status: 200, json: { items } });
    renderScreen();

    await waitFor(() => expect(screen.getAllByTestId(ROW_TEST_ID).length).toBeGreaterThan(0));
    const list = screen.getByTestId('detail-unreleased-all-list');
    expect(list.props.data).toHaveLength(TRACK_COUNT);
    expect(list.props.horizontal).toBeFalsy();
  });

  it('shows the rail first chip group when no where param is given', async () => {
    const items = [genius(OFF_STREAMING_ID, true), genius(ON_STREAMING_ID, false)];
    __http.reply(UNRELEASED, { status: 200, json: { items } });
    renderScreen();

    expect(await screen.findByText(`Full list entry ${OFF_STREAMING_ID}`)).toBeTruthy();
    expect(screen.queryByText(`Full list entry ${ON_STREAMING_ID}`)).toBeNull();
  });

  it('shows the other group when the where param asks for it', async () => {
    const items = [genius(OTHER_OFF_STREAMING_ID, true), genius(OTHER_ON_STREAMING_ID, false)];
    __http.reply(UNRELEASED, { status: 200, json: { items } });
    renderScreen('unreleased');

    expect(await screen.findByText(`Full list entry ${OTHER_ON_STREAMING_ID}`)).toBeTruthy();
    expect(screen.queryByText(`Full list entry ${OTHER_OFF_STREAMING_ID}`)).toBeNull();
  });

  describe('sort and filters', () => {
    beforeEach(() => {
      __http.reply(EVENTS, { status: 204 });
    });

    it('reorders by Best quality, narrows to Playable, and logs both picks', async () => {
      const items = [
        ranked(LOW_ID, LOW_RANK, true),
        ranked(HIGH_ID, HIGH_RANK, true),
        ranked(NO_LINK_ID, HIGH_RANK + 1, false),
      ];
      __http.reply(UNRELEASED, { status: 200, json: { items } });
      renderScreen();
      await screen.findByText(`Full list entry ${LOW_ID}`);

      pick('detail-unreleased-sort');
      pick('detail-unreleased-sort-best_quality');
      pick('detail-unreleased-all-what-playable');

      const titles = [`Full list entry ${HIGH_ID}`, `Full list entry ${LOW_ID}`];
      expect(shownTitles()).toEqual(titles);
      await waitFor(() =>
        expect(recordedActions()).toEqual(
          expect.arrayContaining([
            'detail.unreleased.sort.best_quality',
            'detail.unreleased.filter.playable',
          ]),
        ),
      );
    });

    it('says no playable tracks when Playable leaves nothing', async () => {
      __http.reply(UNRELEASED, { status: 200, json: { items: [ranked(NO_LINK_ID, 0, false)] } });
      renderScreen();
      await screen.findByText(`Full list entry ${NO_LINK_ID}`);

      pick('detail-unreleased-all-what-playable');

      expect(screen.getByText('No playable tracks for this artist')).toBeTruthy();
    });

    it('shows the list with a Still finding more footer while pending, not a skeleton', async () => {
      const json = { items: [genius(PENDING_ID, true)], pending: true };
      __http.reply(UNRELEASED, { status: 200, json });
      renderScreen();

      expect(await screen.findByText(`Full list entry ${PENDING_ID}`)).toBeTruthy();
      expect(screen.getByText('Still finding more')).toBeTruthy();
      expect(screen.queryByTestId('detail-discography-skeleton')).toBeNull();
    });
  });
});
