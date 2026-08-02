import { addFavorite, listFavorites, removeFavorite } from '../favorites';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

describe('listFavorites', () => {
  it('GETs the discovery favorites collection and resolves the items and total', async () => {
    __http.reply('GET /v1/discovery/favorites', {
      status: 200,
      json: {
        items: [{ kind: 'artist', key: 'don toliver', title: 'Don Toliver' }],
        total: 1,
      },
    });

    await expect(listFavorites()).resolves.toEqual({
      items: [{ kind: 'artist', key: 'don toliver', title: 'Don Toliver' }],
      total: 1,
    });
    expect(__http.last().method).toBe('GET');
    expect(__http.last().path).toBe('/v1/discovery/favorites');
  });
});

describe('addFavorite', () => {
  it('PUTs the entity by kind/title/subtitle and lets the server derive the key it answers with', async () => {
    __http.reply('PUT /v1/discovery/favorites', {
      status: 200,
      json: { kind: 'track', key: 'don toliver|no idea', title: 'No Idea', subtitle: 'Don Toliver' },
    });

    const added = await addFavorite({
      kind: 'track',
      title: 'No Idea',
      subtitle: 'Don Toliver',
      image_url: 'https://img/1.jpg',
    });

    expect(added.key).toBe('don toliver|no idea');
    expect(__http.last().method).toBe('PUT');
    expect(JSON.parse(__http.last().body)).toEqual({
      kind: 'track',
      title: 'No Idea',
      subtitle: 'Don Toliver',
      image_url: 'https://img/1.jpg',
    });
  });

  it('never sends a client-derived key — the request body carries only the entity it names', async () => {
    __http.reply('PUT /v1/discovery/favorites', {
      status: 200,
      json: { kind: 'artist', key: 'don toliver', title: 'Don Toliver' },
    });

    await addFavorite({ kind: 'artist', title: 'Don Toliver', subtitle: '' });

    expect(JSON.parse(__http.last().body)).not.toHaveProperty('key');
    expect(JSON.parse(__http.last().body)).not.toHaveProperty('favorite_key');
  });
});

describe('removeFavorite', () => {
  it('DELETEs with the entity in the body and resolves undefined on the 204', async () => {
    __http.reply('DELETE /v1/discovery/favorites', { status: 204 });

    await expect(
      removeFavorite({ kind: 'album', title: 'Heaven Or Hell', subtitle: 'Don Toliver' }),
    ).resolves.toBeUndefined();
    expect(__http.last().method).toBe('DELETE');
    expect(JSON.parse(__http.last().body)).toEqual({
      kind: 'album',
      title: 'Heaven Or Hell',
      subtitle: 'Don Toliver',
    });
  });
});
