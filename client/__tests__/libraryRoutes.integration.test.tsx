import { Text } from 'react-native';
import { act, fireEvent, renderRouter, screen } from 'expo-router/testing-library';

import { FeaturingRoute, LibraryRoute, PlaylistRoute } from '@/mobile';
import { trackResponseFactory } from '@testing/factories';
import {
  createPlaybackWrapper,
  createTestQueryClient,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

const PAGE_LIMIT = 100;

function page(items: unknown[]) {
  return {
    status: 200,
    json: { items, total: items.length, limit: PAGE_LIMIT, offset: 0, has_more: false },
  };
}

const ROUTES = {
  '(tabs)/library/index': LibraryRoute,
  '(tabs)/library/playlist/[id]': PlaylistRoute,
  '(tabs)/library/featuring': FeaturingRoute,
  '(tabs)/discover/featuring': FeaturingRoute,
  '(tabs)/discover/index': () => <Text>discover-screen</Text>,
  '(tabs)/library/detail': () => <Text>library-detail-screen</Text>,
  '(tabs)/discover/detail': () => <Text>discover-detail-screen</Text>,
};

function playlistRequests(): string[] {
  return (__http.requests as { path: string }[])
    .map((r) => r.path)
    .filter((path) => path.startsWith('/v1/playlists/'));
}

async function openAt(url: string) {
  const wrapper = createPlaybackWrapper(createTestQueryClient({ mutations: true }));
  const rendered = renderRouter(ROUTES, { initialUrl: url, wrapper });
  await act(async () => {});
  return rendered;
}

beforeEach(async () => {
  await signInTestUser();
  __http.replyAll(page([]));
});

describe('the playlist route', () => {
  it.each(['p1/tracks', '../p1', 'p1?x=1', 'p1#frag', 'p1%2Ftracks'])(
    'redirects to the library and requests nothing for the malformed id %p',
    async (id) => {
      const { getPathname } = await openAt(`/library/playlist/${encodeURIComponent(id)}`);

      expect(getPathname()).toBe('/library');
      expect(playlistRequests()).toEqual([]);
    },
  );

  it('fetches the playlist for a well-formed id, so the redirect above is not a blanket one', async () => {
    __http.reply('GET /v1/playlists/p1', { status: 404, json: { message: 'not found' } });

    const { getPathname } = await openAt('/library/playlist/p1');

    expect(playlistRequests()).toEqual(['/v1/playlists/p1']);
    expect(getPathname()).toBe('/library/playlist/p1');
  });
});

function featuringRequestQuery(): URLSearchParams {
  const [request] = (__http.requests as { path: string; query: string }[]).filter(
    (r) => r.path === '/v1/tracks/featuring',
  );
  return new URLSearchParams(request?.query);
}

describe('the library route', () => {
  it('sends the empty library to the discover tab', async () => {
    const { getPathname } = await openAt('/library');

    fireEvent.press(await screen.findByText('Discover Music'));
    await act(async () => {});

    expect(getPathname()).toBe('/discover');
  });
});

describe('the featuring route', () => {
  it('asks for the tracks featuring the artist its params name', async () => {
    await openAt('/library/featuring?name=Guest%20Star');

    expect(featuringRequestQuery().get('name')).toBe('Guest Star');
  });

  it('passes the musicbrainz and deezer ids its params carry on to the request', async () => {
    await openAt('/library/featuring?name=Guest%20Star&mbid=mb-1&deezer_id=42');

    const query = featuringRequestQuery();
    expect(query.get('mbid')).toBe('mb-1');
    expect(query.get('deezer_id')).toBe('42');
  });

  it.each([
    ['library', '/library/featuring', '/library/detail'],
    ['discover', '/discover/featuring', '/discover/detail'],
  ])('opens a tapped track on the detail screen of the %s tab', async (_tab, url, detailPath) => {
    const track = trackResponseFactory.build({
      title: 'Aerodynamic',
      acquisition_status: 'pending',
    });
    __http.reply('GET /v1/tracks/featuring', page([track]));

    const { getPathname } = await openAt(`${url}?name=Guest%20Star`);
    fireEvent.press(await screen.findByTestId(`library-row-${track.id}`));
    await act(async () => {});

    expect(getPathname()).toBe(detailPath);
  });
});
