import { Linking } from 'react-native';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
import { DetailPortProvider } from '@features/detail/ports/DetailPortProvider';
import { ArtistUnreleased } from '@features/detail/ui/ArtistUnreleased';
import { createTestQueryClient, createWrapper, signInTestUser } from '@testing/queryHarness';

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
};

const POLL_MS = 10_000;
const GIVE_UP_AFTER_MS = 300_000;
const MAX_REQUESTS = 20;
const SECOND_REQUEST = 2;

const SILENT = { ...CANCUN, genius_id: 456, title: 'Silent Demo', has_media: false };

function renderSection() {
  const Base = createWrapper(createTestQueryClient());
  return render(
    <Base>
      <DetailPortProvider port={httpDetailPort}>
        <ArtistUnreleased artist={ARTIST} />
      </DetailPortProvider>
    </Base>,
  );
}

beforeEach(signInTestUser);

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
    expect(screen.queryByTestId('detail-unreleased-no-audio-123')).toBeNull();
    expect(screen.getByTestId('detail-unreleased-no-audio-456')).toBeTruthy();

    fireEvent.press(screen.getByTestId('detail-unreleased-123'));
    expect(openURL).toHaveBeenCalledWith(CANCUN.genius_url);
    openURL.mockRestore();
  });

  it('stays hidden when the request fails', async () => {
    __http.reply(UNRELEASED, { status: 500, json: { code: 'internal' } });
    renderSection();

    await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(1));
    expect(screen.queryByTestId('detail-unreleased')).toBeNull();
  });

  it('stays hidden when the endpoint answers 404', async () => {
    __http.reply(UNRELEASED, { status: 404, json: { code: 'not_found' } });
    renderSection();

    await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(1));
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

  it('shows the header and a hint while the first fetch is in flight', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN] } });
    renderSection();

    expect(screen.getByTestId('detail-unreleased-loading')).toBeTruthy();
    expect(screen.getByText('UNRELEASED')).toBeTruthy();
    expect(screen.getByText('Looking for unreleased tracks…')).toBeTruthy();
    expect(await screen.findByText('Cancun')).toBeTruthy();
    expect(screen.queryByText('Looking for unreleased tracks…')).toBeNull();
  });

  it('shows no audio hint only when has_media is exactly false', async () => {
    const unknown = { ...CANCUN, genius_id: 789, title: 'Unknown Demo', has_media: null };
    __http.reply(UNRELEASED, { status: 200, json: { items: [SILENT, unknown] } });
    renderSection();

    expect(await screen.findByText('Unknown Demo')).toBeTruthy();
    expect(screen.getByTestId('detail-unreleased-no-audio-456')).toBeTruthy();
    expect(screen.queryByTestId('detail-unreleased-no-audio-789')).toBeNull();
  });

  it('keeps the loading line and refetches every 10s while pending, then shows the row', async () => {
    jest.useFakeTimers();
    try {
      __http.replyOnce(UNRELEASED, { status: 200, json: { items: [], pending: true } });
      __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN], pending: false } });
      renderSection();

      await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(1));
      expect(screen.getByText('Looking for unreleased tracks…')).toBeTruthy();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(POLL_MS);
      });

      expect(await screen.findByText('Cancun')).toBeTruthy();
      expect(__http.countFor(UNRELEASED)).toBe(SECOND_REQUEST);
      expect(screen.queryByText('Looking for unreleased tracks…')).toBeNull();
    } finally {
      jest.useRealTimers();
    }
  });

  it('gives up and hides after about three minutes of pending', async () => {
    jest.useFakeTimers();
    try {
      __http.reply(UNRELEASED, { status: 200, json: { items: [], pending: true } });
      renderSection();
      await waitFor(() => expect(__http.countFor(UNRELEASED)).toBe(1));

      await act(async () => {
        await jest.advanceTimersByTimeAsync(GIVE_UP_AFTER_MS);
      });

      expect(screen.queryByTestId('detail-unreleased-loading')).toBeNull();
      expect(__http.countFor(UNRELEASED)).toBeLessThanOrEqual(MAX_REQUESTS);
    } finally {
      jest.useRealTimers();
    }
  });

  it('lays the tracks out in a horizontal row', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [CANCUN, SILENT] } });
    renderSection();

    const rail = await screen.findByTestId('detail-unreleased-rail');
    expect(rail.props.horizontal).toBe(true);
  });

  it('drops the loading hint when the request fails', async () => {
    __http.reply(UNRELEASED, { status: 500, json: { code: 'internal' } });
    renderSection();

    await waitFor(() => expect(screen.queryByTestId('detail-unreleased-loading')).toBeNull());
    expect(screen.queryByText('Looking for unreleased tracks…')).toBeNull();
  });

  it('drops the loading hint when the artist has none', async () => {
    __http.reply(UNRELEASED, { status: 200, json: { items: [] } });
    renderSection();

    await waitFor(() => expect(screen.queryByTestId('detail-unreleased-loading')).toBeNull());
    expect(screen.queryByTestId('detail-unreleased')).toBeNull();
  });
});
