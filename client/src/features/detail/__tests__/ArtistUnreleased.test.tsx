import { AccessibilityInfo, Linking } from 'react-native';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
import { DetailPortProvider } from '@features/detail/ports/DetailPortProvider';
import { ArtistUnreleased } from '@features/detail/ui/ArtistUnreleased';
import {
  createPlaybackWrapper,
  createTestQueryClient,
  idlePlayback,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

const UNRELEASED = 'GET /v1/discovery/artists/spotify/artist-1/unreleased';

const ARTIST: DiscoveryResult = {
  kind: 'artist',
  title: 'Juice WRLD',
  subtitle: null,
  image_url: null,
  confidence: 'high',
  sources: [{ provider: 'spotify', external_id: 'artist-1', url: 'https://s.example/1' }],
  extras: {},
};

const CANCUN = {
  genius_id: 123,
  title: 'Cancun',
  cover_url: null,
  genius_url: 'https://genius.com/Juice-wrld-cancun-lyrics',
  release_date: '2022-07-24',
  has_media: true,
  audio_links: [],
  extras: {},
};

const POLL_MS = 10_000;
const GIVE_UP_AFTER_MS = 300_000;
const MAX_REQUESTS = 20;
const SECOND_REQUEST = 2;
const POLLS_AFTER_RETRY = 3;

const SILENT = { ...CANCUN, genius_id: 456, title: 'Silent Demo', has_media: false };

type RecordedRequest = { method: string; path: string; body: unknown };

const startQueue = jest.fn<Promise<void>, unknown[]>(() => Promise.resolve());
const PLAYBACK = { ...idlePlayback, startQueue };

function renderSection() {
  return renderSectionFor(ARTIST);
}

function renderSectionFor(artist: DiscoveryResult) {
  const Base = createPlaybackWrapper(createTestQueryClient({ mutations: true }), PLAYBACK);
  return render(
    <Base>
      <DetailPortProvider port={httpDetailPort}>
        <ArtistUnreleased artist={artist} />
      </DetailPortProvider>
    </Base>,
  );
}

beforeEach(() => {
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(true);
  startQueue.mockClear();
  signInTestUser();
});

const SOUNDCLOUD_URL = 'https://soundcloud.com/ken/dream';
const DREAM = {
  ...CANCUN,
  genius_id: 789,
  title: 'Dream',
  audio_links: [{ provider: 'soundcloud', external_id: SOUNDCLOUD_URL, url: SOUNDCLOUD_URL }],
};
const OWNED_LOOT = {
  ...DREAM,
  genius_id: 790,
  title: 'Loot',
  extras: { owned_track_id: 'track-owned-loot', owned_acquisition_status: 'ready' },
};

function savedTrack() {
  return {
    status: 201,
    json: {
      id: 'track-9',
      title: 'Dream',
      artist: 'Juice WRLD',
      album: 'UNRELEASED',
      duration_seconds: null,
      added_at: '2026-10-24T00:00:00Z',
      acquisition_status: 'pending',
      artwork_url: null,
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
    },
  };
}

const V3_LOSSLESS = /V3.*Lossless/;
const V10_CD = /V10.*CD Quality/;

describe('ArtistUnreleased', () => {
  it('stays hidden when the artist has no unreleased tracks', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [] } });
    renderSection();

    await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(1));
    expect(screen.queryByTestId('detail-unreleased')).toBeNull();
  });

  it('lists a row per track, flags the ones without audio, and opens Genius on tap', async () => {
    const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN, SILENT] } });
    renderSection();

    expect(await screen.findByText('Cancun')).toBeTruthy();
    expect(screen.getByText('Silent Demo')).toBeTruthy();
    expect(screen.queryByTestId('detail-unreleased-hint-123')).toBeNull();
    expect(screen.getByTestId('detail-unreleased-hint-456')).toBeTruthy();

    fireEvent.press(screen.getByTestId('detail-unreleased-123'));
    expect(openURL).toHaveBeenCalledWith(CANCUN.genius_url);
    openURL.mockRestore();
  });

  it('offers a retry instead of rows when the request fails', async () => {
    __http.reply(UNRELEASED, { status: 500, json: { code: 'internal' } });
    renderSection();

    expect(await screen.findByText("Couldn't load unreleased tracks")).toBeTruthy();
    expect(screen.queryByTestId('detail-unreleased')).toBeNull();
  });

  it('offers a retry when the endpoint answers 404', async () => {
    __http.reply(UNRELEASED, { status: 404, json: { code: 'not_found' } });
    renderSection();

    expect(await screen.findByText("Couldn't load unreleased tracks")).toBeTruthy();
    expect(screen.queryByTestId('detail-unreleased')).toBeNull();
  });

  it('renders a row whose release_date is null', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [{ ...CANCUN, release_date: null }] } });
    renderSection();

    expect(await screen.findByText('Cancun')).toBeTruthy();
  });

  it('drops a row with a non-https url and keeps the others', async () => {
    const insecure = { ...SILENT, genius_url: 'http://genius.com/x' };
    __http.reply(UNRELEASED, { status: 200, json: { items: [insecure, CANCUN] } });
    renderSection();

    expect(await screen.findByText('Cancun')).toBeTruthy();
    expect(screen.queryByText('Silent Demo')).toBeNull();
  });

  it('survives a rejected openURL', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    jest.spyOn(Linking, 'openURL').mockRejectedValue(new Error('no handler'));
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN] } });
    renderSection();

    fireEvent.press(await screen.findByTestId('detail-unreleased-123'));
    await waitFor(() => expect(warn).toHaveBeenCalled());
  });

  it('shows the header and card skeletons while the first fetch is in flight', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN] } });
    renderSection();

    expect(screen.getByTestId('detail-unreleased-loading')).toBeTruthy();
    expect(screen.getByText('NOT ON STREAMING')).toBeTruthy();
    expect(
      screen.getByTestId('detail-discography-skeleton', { includeHiddenElements: true }),
    ).toBeTruthy();
    expect(await screen.findByText('Cancun')).toBeTruthy();
    expect(screen.queryByTestId('detail-unreleased-loading')).toBeNull();
  });

  it('shows no audio hint only when has_media is exactly false', async () => {
    const unknown = { ...CANCUN, genius_id: 789, title: 'Unknown Demo', has_media: null };
    __http.reply(UNRELEASED, { status: 200, json: { items: [SILENT, unknown] } });
    renderSection();

    expect(await screen.findByText('Unknown Demo')).toBeTruthy();
    expect(screen.getByTestId('detail-unreleased-hint-456')).toBeTruthy();
    expect(screen.queryByTestId('detail-unreleased-hint-789')).toBeNull();
  });

  it('keeps the skeletons and refetches every 10s while pending, then shows the row', async () => {
    jest.useFakeTimers();
    try {
      __http.replyOnce(UNRELEASED, { status: 200, json: { items: [], pending: true } });
      __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN], pending: false } });
      renderSection();

      await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(1));
      expect(screen.getByTestId('detail-unreleased-loading')).toBeTruthy();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(POLL_MS);
      });

      expect(await screen.findByText('Cancun')).toBeTruthy();
      expect(__http.countFor(UNRELEASED)).toBe(SECOND_REQUEST);
      expect(screen.queryByTestId('detail-unreleased-loading')).toBeNull();
    } finally {
      jest.useRealTimers();
    }
  });

  it('gives up and offers a retry after about three minutes of pending', async () => {
    jest.useFakeTimers();
    try {
      __http.reply(UNRELEASED, { status: 200, json: { items: [], pending: true } });
      renderSection();
      await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(1));

      await act(async () => {
        await jest.advanceTimersByTimeAsync(GIVE_UP_AFTER_MS);
      });

      expect(screen.queryByTestId('detail-unreleased-loading')).toBeNull();
      expect(screen.getByText("Couldn't load unreleased tracks")).toBeTruthy();
      expect(__http.countFor(UNRELEASED)).toBeLessThanOrEqual(MAX_REQUESTS);
    } finally {
      jest.useRealTimers();
    }
  });

  it('splits off-streaming releases from unreleased tracks behind chips', async () => {
    const offStreaming = { ...CANCUN, genius_id: 789, title: 'Do You Want', off_streaming: true };
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN, offStreaming] } });
    renderSection();

    expect(await screen.findByText('Do You Want')).toBeTruthy();
    expect(screen.queryByText('Cancun')).toBeNull();
    fireEvent.press(screen.getByTestId('detail-unreleased-group-unreleased'));
    expect(screen.getByText('Cancun')).toBeTruthy();
    expect(screen.queryByText('Do You Want')).toBeNull();
  });

  it('lays the tracks out in a horizontal row', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN, SILENT] } });
    renderSection();

    const rail = await screen.findByTestId('detail-unreleased-rail');
    expect(rail.props.horizontal).toBe(true);
  });

  it('drops the skeletons when the request fails', async () => {
    __http.reply(UNRELEASED, { status: 500, json: { code: 'internal' } });
    renderSection();
    expect(screen.getByTestId('detail-unreleased-loading')).toBeTruthy();

    await waitFor(() => expect(screen.queryByTestId('detail-unreleased-loading')).toBeNull());
    expect(screen.getByTestId('detail-unreleased-retry')).toBeTruthy();
  });

  it('refetches on Retry and shows the rows once the request succeeds', async () => {
    __http.replyOnce(UNRELEASED, { status: 500, json: { code: 'internal' } });
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN] } });
    renderSection();

    fireEvent.press(await screen.findByTestId('detail-unreleased-retry'));

    expect(await screen.findByText('Cancun')).toBeTruthy();
    expect(__http.countFor(UNRELEASED)).toBe(SECOND_REQUEST);
  });

  it('polls again within a fresh budget after Retry on a spent pending answer', async () => {
    jest.useFakeTimers();
    try {
      __http.reply(UNRELEASED, { status: 200, json: { items: [], pending: true } });
      renderSection();
      await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(1));
      await act(async () => {
        await jest.advanceTimersByTimeAsync(GIVE_UP_AFTER_MS);
      });
      const spent = __http.countFor(UNRELEASED);

      __http.reply(UNRELEASED, { status: 200, json: { items: [], pending: true } });
      fireEvent.press(screen.getByTestId('detail-unreleased-retry'));
      await act(async () => {
        await jest.advanceTimersByTimeAsync(POLL_MS * POLLS_AFTER_RETRY);
      });

      expect(__http.countFor(UNRELEASED)).toBeGreaterThanOrEqual(spent + POLLS_AFTER_RETRY);
      expect(screen.getByTestId('detail-unreleased-loading')).toBeTruthy();
    } finally {
      jest.useRealTimers();
    }
  });

  it('hides the row when Retry returns a finished empty answer', async () => {
    __http.replyOnce(UNRELEASED, { status: 500, json: { code: 'internal' } });
    __http.reply(UNRELEASED, { status: 200, json: { items: [] } });
    renderSection();

    fireEvent.press(await screen.findByTestId('detail-unreleased-retry'));

    await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(SECOND_REQUEST));
    await waitFor(() => expect(screen.queryByTestId('detail-unreleased-retry')).toBeNull());
    expect(screen.queryByTestId('detail-unreleased')).toBeNull();
  });

  it('drops the loading hint when the artist has none', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [] } });
    renderSection();
    expect(screen.getByTestId('detail-unreleased-loading')).toBeTruthy();

    await waitFor(() => expect(screen.queryByTestId('detail-unreleased-loading')).toBeNull());
    expect(screen.queryByTestId('detail-unreleased')).toBeNull();
  });

  it('saves a song with a SoundCloud link as an UNRELEASED track pinned to that link', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [DREAM] } });
    __http.reply('POST /v1/tracks', savedTrack());
    renderSection();

    expect(await screen.findByText('Tap to save')).toBeTruthy();
    fireEvent.press(screen.getByTestId('detail-unreleased-789'));

    await waitFor(() => expect(__http.countFor('POST /v1/tracks')).toBe(1));
    const save = __http.requests.find(
      (request: RecordedRequest) => request.method === 'POST' && request.path === '/v1/tracks',
    );
    const sent = JSON.parse(String(save.body));
    expect(sent).toMatchObject({
      title: 'Dream',
      artist: 'Juice WRLD',
      album: 'UNRELEASED',
      source_url: SOUNDCLOUD_URL,
    });
    expect(await screen.findByText('Downloading…')).toBeTruthy();
  });

  it('plays a song the user already owns instead of saving it again', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [OWNED_LOOT] } });
    renderSection();

    expect(await screen.findByText('Tap to play')).toBeTruthy();
    fireEvent.press(screen.getByTestId('detail-unreleased-790'));

    await waitFor(() => expect(startQueue).toHaveBeenCalledTimes(1));
    expect(__http.countFor('POST /v1/tracks')).toBe(0);
  });

  it('shows no versions line for a card with a single version', async () => {
    const single = { ...DREAM, genius_id: 802, title: 'Quiet', versions: [{ title: 'Quiet' }] };
    __http.reply(UNRELEASED, { status: 200, json: { items: [single] } });
    renderSection();

    expect(await screen.findByText('Tap to save')).toBeTruthy();
    expect(screen.queryByTestId('detail-unreleased-versions-802')).toBeNull();
  });

  it('renders a tracker-only song with its quality and saves it pinned to the pixeldrain file', async () => {
    const trackerOnly = {
      genius_id: null,
      tracker_key: 'ken-carson:dream-v2',
      title: 'Dream [V2]',
      quality: 'High Quality',
      cover_url: null,
      genius_url: null,
      release_date: null,
      has_media: true,
      audio_links: [
        {
          provider: 'pixeldrain',
          external_id: 'ozSpSjn7',
          url: 'https://pixeldrain.com/u/ozSpSjn7',
        },
      ],
      extras: {},
    };
    __http.reply(UNRELEASED, { status: 200, json: { items: [trackerOnly] } });
    __http.reply('POST /v1/tracks', savedTrack());
    renderSection();

    expect(await screen.findByText('High Quality')).toBeTruthy();
    fireEvent.press(screen.getByTestId('detail-unreleased-ken-carson:dream-v2'));

    await waitFor(() => expect(__http.countFor('POST /v1/tracks')).toBe(1));
    const save = __http.requests.find(
      (request: RecordedRequest) => request.method === 'POST' && request.path === '/v1/tracks',
    );
    expect(JSON.parse(String(save.body))).toMatchObject({
      title: 'Dream [V2]',
      album: 'UNRELEASED',
      sources: [{ provider: 'pixeldrain', external_id: 'ozSpSjn7' }],
    });
  });

  it('shows Open on <host> for a link-out track with no audio links and opens that URL on tap', async () => {
    const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);
    const linkOut = {
      ...CANCUN,
      genius_id: null,
      genius_url: null,
      tracker_key: 'ken:dream-v2',
      title: 'Dream V2',
      open_url: 'https://mega.nz/file/abc',
      open_host: 'mega.nz',
    };
    __http.reply(UNRELEASED, { status: 200, json: { items: [linkOut] } });
    renderSection();

    expect(await screen.findByText('Open on mega.nz')).toBeTruthy();
    fireEvent.press(screen.getByTestId('detail-unreleased-ken:dream-v2'));
    expect(openURL).toHaveBeenCalledWith('https://mega.nz/file/abc');
    openURL.mockRestore();
  });

  it('falls back to the artist real photo for a track with no cover, and to the placeholder without one', async () => {
    const photo = 'https://img.example/ken.jpg';
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN] } });
    const withPhoto = renderSectionFor({ ...ARTIST, image_url: photo });
    const shown = await withPhoto.findByLabelText('Cancun');
    expect(shown.props.source).toEqual([{ uri: photo }]);
    withPhoto.unmount();

    const lastfm = 'https://lastfm.example/2a96cbd8b46e442fc41c2b86b821562f.png';
    for (const image_url of [null, lastfm]) {
      __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN] } });
      const bare = renderSectionFor({ ...ARTIST, image_url });
      const cover = await bare.findByLabelText('Cancun');
      expect(JSON.stringify(cover.props.source)).not.toContain('uri');
      bare.unmount();
    }
  });

  it('says how many versions a card groups and saves the best version under its full title', async () => {
    function version(title: string, links: unknown[]) {
      return { title, audio_links: links };
    }
    const grouped = {
      ...DREAM,
      genius_id: 801,
      title: 'Sports Car',
      best_version_title: 'Sports Car [V10]',
      versions: [
        version('Sports Car [V1]', []),
        version('Sports Car [V10]', DREAM.audio_links),
        version('Sports Car [V2]', []),
      ],
    };
    __http.reply(UNRELEASED, { status: 200, json: { items: [grouped] } });
    __http.reply('POST /v1/tracks', savedTrack());
    renderSection();

    expect(await screen.findByText('3 versions')).toBeTruthy();
    expect(screen.getByText('Tap to save')).toBeTruthy();
    fireEvent.press(screen.getByTestId('detail-unreleased-801'));

    await waitFor(() => expect(__http.countFor('POST /v1/tracks')).toBe(1));
    const save = __http.requests.find(
      (request: RecordedRequest) => request.method === 'POST' && request.path === '/v1/tracks',
    );
    expect(JSON.parse(String(save.body))).toMatchObject({ title: 'Sports Car [V10]' });
  });

  it('opens a sheet listing each version with its quality on a long-press', async () => {
    const grouped = {
      ...DREAM,
      genius_id: 811,
      title: 'Vault Sheet Demo',
      versions: [
        { title: 'Vault Sheet Demo [V3]', label: 'V3', quality: 'Lossless' },
        { title: 'Vault Sheet Demo [V10]', label: 'V10', quality: 'CD Quality' },
        { title: 'Vault Sheet Demo [V14]', label: null, quality: null },
      ],
    };
    __http.reply(UNRELEASED, { status: 200, json: { items: [grouped] } });
    renderSection();

    fireEvent(await screen.findByTestId('detail-unreleased-811'), 'longPress');

    expect(screen.getByTestId('detail-unreleased-version-811-0')).toHaveTextContent(V3_LOSSLESS);
    expect(screen.getByTestId('detail-unreleased-version-811-1')).toHaveTextContent(V10_CD);
    expect(screen.getByTestId('detail-unreleased-version-811-2')).toHaveTextContent(
      'Vault Sheet Demo [V14]',
    );
  });

  it('opens no sheet on a long-press of a card with one version', async () => {
    const single = { ...DREAM, genius_id: 812, title: 'Lone Vault Demo', versions: [] };
    __http.reply(UNRELEASED, { status: 200, json: { items: [single] } });
    renderSection();

    fireEvent(await screen.findByTestId('detail-unreleased-812'), 'longPress');

    expect(screen.queryByTestId('detail-unreleased-versions-sheet-812')).toBeNull();
  });
});
