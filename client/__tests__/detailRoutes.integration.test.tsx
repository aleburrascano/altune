import { Text } from 'react-native';
import { act, fireEvent, renderRouter, screen } from 'expo-router/testing-library';

import type { DiscoveryResult } from '@shared/catalog';
import { detailHref } from '@shared/navigation';
import { DetailRoute, UnreleasedRoute } from '@/mobile';
import {
  createPlaybackWrapper,
  createTestQueryClient,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

const ROUTES = {
  '(tabs)/discover/index': () => <Text>discover-screen</Text>,
  '(tabs)/discover/detail': DetailRoute,
  '(tabs)/library/index': () => <Text>library-screen</Text>,
  '(tabs)/library/detail': DetailRoute,
  '(tabs)/discover/unreleased': UnreleasedRoute,
};

const ARTIST: DiscoveryResult = {
  kind: 'artist',
  title: 'Route Artist',
  subtitle: null,
  image_url: null,
  confidence: 'high',
  sources: [{ provider: 'spotify', external_id: 'artist-1', url: 'https://s.example/1' }],
  extras: {},
};

function geniusEntry(id: number, offStreaming: boolean) {
  return {
    genius_id: id,
    title: `Route entry ${id}`,
    cover_url: null,
    genius_url: `https://genius.com/route-entry-${id}-lyrics`,
    release_date: null,
    has_media: true,
    audio_links: [],
    off_streaming: offStreaming,
    extras: {},
  };
}

function urlFor(path: string, params: Record<string, string>): string {
  return `${path}?${new URLSearchParams(params).toString()}`;
}

async function openAt(url: string) {
  const wrapper = createPlaybackWrapper(createTestQueryClient({ mutations: true }));
  const rendered = renderRouter(ROUTES, { initialUrl: url, wrapper });
  await act(async () => {});
  return rendered;
}

beforeEach(async () => {
  await signInTestUser();
  __http.replyAll({ status: 200, json: { items: [], total: 0 } });
});

describe('the detail route', () => {
  it('shows the detail of the result its handoff param names', async () => {
    const { params } = detailHref('/discover/detail', ARTIST, 'search-1');
    await openAt(urlFor('/discover/detail', { handoff: params.handoff }));

    expect(await screen.findByText('Route Artist')).toBeTruthy();
  });

  it('replaces itself with the tab root it was opened under when there is nothing to go back to', async () => {
    const { params } = detailHref('/library/detail', ARTIST, 'search-1');
    const { getPathname } = await openAt(urlFor('/library/detail', { handoff: params.handoff }));
    fireEvent.press(await screen.findByTestId('detail-back'));
    await act(async () => {});

    expect(getPathname()).toBe('/library');
  });

  it('redirects to discover when the param names no handoff', async () => {
    const { getPathname } = await openAt(urlFor('/library/detail', { handoff: 'unknown' }));

    expect(getPathname()).toBe('/discover');
    expect(screen.getByText('discover-screen')).toBeTruthy();
  });
});

describe('the unreleased route', () => {
  it('shows the artist the handoff names', async () => {
    const { params } = detailHref('/discover/unreleased', ARTIST);
    await openAt(urlFor('/discover/unreleased', { handoff: params.handoff }));

    expect(await screen.findByTestId('detail-unreleased-all')).toBeTruthy();
  });

  it('opens on the group its where param names', async () => {
    __http.reply('GET /v1/discovery/artists/spotify/artist-1/unreleased', {
      status: 200,
      json: { items: [geniusEntry(1, true), geniusEntry(2, false)] },
    });
    const { params } = detailHref('/discover/unreleased', ARTIST);
    await openAt(urlFor('/discover/unreleased', { handoff: params.handoff, where: 'unreleased' }));

    expect(await screen.findByText('Route entry 2')).toBeTruthy();
    expect(screen.queryByText('Route entry 1')).toBeNull();
  });

  it('redirects to discover when the param names no handoff', async () => {
    const { getPathname } = await openAt(urlFor('/discover/unreleased', { handoff: 'gone' }));

    expect(getPathname()).toBe('/discover');
  });
});
