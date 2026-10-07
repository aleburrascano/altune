import type { Favorite, FavoriteRef } from '@shared/api-client/favorites';
import { asFavoriteKey } from '@shared/api-client/ids';
import { supabase } from '@shared/auth/supabaseClient';

import { httpFavoritesPort } from '@shared/favorites/adapters/http/httpFavoritesPort';
import { createMemoryFavoritesPort } from '@shared/favorites/__tests__/memoryFavoritesPort';
import type { FavoritesPort } from '@shared/favorites/ports/favoritesPort';

const { __http, fakeFetch } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const FAVORITES_PATH = '/v1/discovery/favorites';
const NO_CONTENT_STATUS = 204;

const artist: FavoriteRef = { kind: 'artist', title: 'Artist One', subtitle: 'Artist' };
const lookAlike: FavoriteRef = { ...artist, kind: 'album' };

function wireFavorite(ref: FavoriteRef): Favorite {
  return {
    kind: ref.kind,
    key: asFavoriteKey(ref.title),
    title: ref.title,
    subtitle: ref.subtitle,
  };
}

function serveFavoritesOverHttp(): FavoritesPort {
  let stored: Favorite[] = [];
  global.fetch = ((url: string, init?: RequestInit) => {
    const method = String(init?.method ?? 'GET').toUpperCase();
    const path = String(url).replace(/^[a-z]+:\/\/[^/]+/, '');
    if (path === FAVORITES_PATH) {
      const body = init?.body === undefined ? undefined : JSON.parse(String(init.body));
      if (method === 'GET') {
        __http.replyOnce(`GET ${FAVORITES_PATH}`, {
          json: { items: stored, total: stored.length },
        });
      } else if (method === 'PUT') {
        const added = wireFavorite(body);
        if (!stored.some((f) => f.kind === added.kind && f.key === added.key)) {
          stored = [added, ...stored];
        }
        __http.replyOnce(`PUT ${FAVORITES_PATH}`, { json: added });
      } else if (method === 'DELETE') {
        const removed = wireFavorite(body);
        stored = stored.filter((f) => !(f.kind === removed.kind && f.key === removed.key));
        __http.replyOnce(`DELETE ${FAVORITES_PATH}`, { status: NO_CONTENT_STATUS });
      }
    }
    return fakeFetch(url, init);
  }) as typeof fetch;
  return httpFavoritesPort;
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

afterEach(() => {
  global.fetch = fakeFetch;
  __http.reset();
});

describe.each<[string, () => FavoritesPort]>([
  ['createMemoryFavoritesPort', () => createMemoryFavoritesPort()],
  ['httpFavoritesPort over HTTP', serveFavoritesOverHttp],
])('%s satisfies the FavoritesPort contract', (_name, freshPort) => {
  it('lists an empty store as no items', async () => {
    const port = freshPort();

    expect(await port.listFavorites()).toEqual({ items: [], total: 0 });
  });

  it('lists exactly the one favorite added', async () => {
    const port = freshPort();

    await port.addFavorite(artist);

    const listed = await port.listFavorites();
    expect(listed.total).toBe(1);
    expect(listed.items).toEqual([wireFavorite(artist)]);
  });

  it('lists a favorite added twice once', async () => {
    const port = freshPort();

    await port.addFavorite(artist);
    await port.addFavorite(artist);

    const listed = await port.listFavorites();
    expect(listed.items).toEqual([wireFavorite(artist)]);
    expect(listed.total).toBe(1);
  });

  it('empties the list again once the favorite is removed', async () => {
    const port = freshPort();
    await port.addFavorite(artist);
    expect((await port.listFavorites()).total).toBe(1);

    await port.removeFavorite(artist);

    expect(await port.listFavorites()).toEqual({ items: [], total: 0 });
  });

  it('keeps the original when a look-alike with the same key but another kind is removed', async () => {
    const port = freshPort();
    await port.addFavorite(artist);

    await port.removeFavorite(lookAlike);

    expect((await port.listFavorites()).items).toEqual([wireFavorite(artist)]);
  });
});
