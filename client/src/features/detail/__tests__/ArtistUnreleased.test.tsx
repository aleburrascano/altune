import { Linking } from 'react-native';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

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
});
