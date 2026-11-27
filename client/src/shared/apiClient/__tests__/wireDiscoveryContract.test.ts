import {
  addFavorite,
  getAlbumTracks,
  getArtistContent,
  getDeezerEnrichment,
  getEnrichment,
  getLyrics,
  getRelatedTracks,
  getUnreleasedTracks,
  listFavorites,
  listSearchHistory,
  recordSearchHistory,
  removeFavorite,
  searchDiscovery,
  searchUnreleasedTracks,
  suggestDiscovery,
} from '@shared/apiClient';
import { signedInWith } from '@shared/apiClient/__tests__/sessionLookup';
import { wireFixture } from '@testing/wireFixtures';

const { __http } = require('@testing/doubles/fetch.js');

type Wire = Record<string, unknown> & { items: unknown[] };

function wire(name: string): Wire {
  return wireFixture(name) as Wire;
}

function keysOf(value: unknown): string[] {
  return Object.keys(value as Record<string, unknown>).sort();
}

beforeEach(() => {
  signedInWith();
});

describe('wire fixture discovery_search decodes through searchDiscovery', () => {
  it('keeps every result, section, provider and the paging the server sent', async () => {
    const fixture = wireFixture('discovery_search') as Wire & { results: unknown[] };
    __http.reply('GET /v1/discovery/search', { status: 200, json: fixture });

    const decoded = await searchDiscovery({ q: 'kendrick' });

    expect(decoded.results).toHaveLength(fixture.results.length);
    expect(decoded.results.length).toBeGreaterThan(0);
    expect(decoded.sections.length).toBeGreaterThan(0);
    expect(decoded.providers.length).toBeGreaterThan(0);
    expect(decoded.query).toBe(fixture.query);
    expect(decoded.total).toBe(fixture.total);
    expect(decoded.results[0]!.favorite_key).toBeDefined();
    expect(decoded.results[0]!.result_signature).toBeDefined();
  });
});

describe('wire fixture discovery_suggest decodes through suggestDiscovery', () => {
  it('keeps each suggestion', async () => {
    const fixture = wireFixture('discovery_suggest') as { suggestions: unknown[] };
    __http.reply('GET /v1/discovery/suggest', { status: 200, json: fixture });

    const decoded = await suggestDiscovery({ q: 'kend' });

    expect(decoded.suggestions).toHaveLength(fixture.suggestions.length);
    expect(decoded.suggestions.length).toBeGreaterThan(0);
  });
});

describe('wire fixture discovery_search_history decodes through listSearchHistory', () => {
  it('keeps each entry and the total', async () => {
    const fixture = wire('discovery_search_history');
    __http.reply('GET /v1/discovery/search-history', { status: 200, json: fixture });

    const decoded = await listSearchHistory();

    expect(decoded.items).toHaveLength(fixture.items.length);
    expect(decoded.items.length).toBeGreaterThan(0);
    expect(decoded.total).toBe(fixture.total);
  });
});

describe('request fixture req_discovery_search_history matches what recordSearchHistory sends', () => {
  it('sends exactly the keys the server request carries', async () => {
    __http.reply('POST /v1/discovery/search-history', { status: 204 });

    await recordSearchHistory('kendrick lamar');

    expect(keysOf(JSON.parse(__http.last().body))).toEqual(
      keysOf(wireFixture('req_discovery_search_history')),
    );
  });
});

describe('wire fixtures for favorites decode through the favorites adapter', () => {
  it('listFavorites keeps every entry with its subtitle and image', async () => {
    const fixture = wire('discovery_favorites');
    __http.reply('GET /v1/discovery/favorites', { status: 200, json: fixture });

    const decoded = await listFavorites();

    expect(decoded.items).toHaveLength(fixture.items.length);
    expect(decoded.items[0]!.subtitle).toBeDefined();
    expect(decoded.items[0]!.image_url).toBeDefined();
  });

  it('addFavorite decodes the PUT answer and sends the keys of the request fixture', async () => {
    const answer = wireFixture('discovery_favorite_put') as { title: string };
    __http.reply('PUT /v1/discovery/favorites', { status: 200, json: answer });
    const request = wireFixture('req_discovery_favorite') as {
      kind: 'album';
      title: string;
      subtitle: string;
      image_url: string;
    };

    const decoded = await addFavorite(request);

    expect(decoded.title).toBe(answer.title);
    expect(keysOf(JSON.parse(__http.last().body))).toEqual(keysOf(request));
  });

  it('removeFavorite sends the keys of the request fixture', async () => {
    __http.reply('DELETE /v1/discovery/favorites', { status: 204 });
    const request = wireFixture('req_discovery_favorite') as {
      kind: 'album';
      title: string;
      subtitle: string;
      image_url: string;
    };

    await removeFavorite(request);

    expect(keysOf(JSON.parse(__http.last().body))).toEqual(keysOf(request));
  });
});

describe('wire fixtures for content endpoints decode through the enrichment adapter', () => {
  it('getAlbumTracks keeps every track', async () => {
    const fixture = wire('discovery_album_tracks');
    __http.reply('GET /v1/discovery/albums/deezer/d1/tracks', { status: 200, json: fixture });

    const decoded = await getAlbumTracks({ provider: 'deezer', externalId: 'd1' });

    expect(decoded.items).toHaveLength(fixture.items.length);
    expect(decoded.items.length).toBeGreaterThan(0);
    expect(decoded.status).toBe(fixture.status);
  });

  it('getRelatedTracks keeps every track', async () => {
    const fixture = wire('discovery_related_tracks');
    __http.reply('GET /v1/discovery/tracks/soundcloud/1/related', { status: 200, json: fixture });

    const decoded = await getRelatedTracks('soundcloud', '1');

    expect(decoded.items).toHaveLength(fixture.items.length);
    expect(decoded.items.length).toBeGreaterThan(0);
  });

  it('getArtistContent keeps the top tracks and the albums', async () => {
    const fixture = wireFixture('discovery_artist_content') as {
      top_tracks: { items: unknown[] };
      albums: { items: unknown[] };
    };
    __http.reply('GET /v1/discovery/artists/deezer/1/content', { status: 200, json: fixture });

    const decoded = await getArtistContent('deezer', '1');

    expect(decoded.top_tracks.items).toHaveLength(fixture.top_tracks.items.length);
    expect(decoded.albums.items).toHaveLength(fixture.albums.items.length);
    expect(decoded.top_tracks.items.length).toBeGreaterThan(0);
    expect(decoded.albums.items.length).toBeGreaterThan(0);
  });
});

describe('wire fixtures for enrichment decode through the enrichment adapter', () => {
  it('getEnrichment keeps the year, genres and artwork', async () => {
    const fixture = wireFixture('discovery_enrichment') as { year: number; genres: string[] };
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: fixture });

    const decoded = await getEnrichment({ kind: 'album', title: 'DAMN.' });

    expect(decoded.year).toBe(fixture.year);
    expect(decoded.genres).toEqual(fixture.genres);
  });

  it('getDeezerEnrichment keeps the label and genres', async () => {
    const fixture = wireFixture('discovery_enrichment_deezer') as { label: string };
    __http.reply('GET /v1/discovery/enrichment/deezer', { status: 200, json: fixture });

    const decoded = await getDeezerEnrichment({ kind: 'album', title: 'DAMN.' });

    expect(decoded.label).toBe(fixture.label);
    expect(decoded.genres.length).toBeGreaterThan(0);
  });

  it('getLyrics keeps the plain text, the synced lines and the writers', async () => {
    const fixture = wireFixture('discovery_lyrics') as { plain: string };
    __http.reply('GET /v1/discovery/lyrics', { status: 200, json: fixture });

    const decoded = await getLyrics({ title: 'N.Y. State of Mind' });

    expect(decoded.plain).toBe(fixture.plain);
    expect(decoded.synced_lines.length).toBeGreaterThan(0);
    expect(decoded.writers.length).toBeGreaterThan(0);
  });
});

describe('wire fixtures for unreleased decode through the enrichment adapter', () => {
  it('getUnreleasedTracks drops no row the server sent', async () => {
    const fixture = wire('discovery_artist_unreleased');
    __http.reply('GET /v1/discovery/artists/deezer/1/unreleased', { status: 200, json: fixture });

    const decoded = await getUnreleasedTracks('deezer', '1', { name: 'Playboi Carti' });

    expect(fixture.items.length).toBeGreaterThan(0);
    expect(decoded.items).toHaveLength(fixture.items.length);
  });

  it('searchUnreleasedTracks drops no hit the server sent', async () => {
    const fixture = wire('discovery_unreleased_search');
    __http.reply('GET /v1/discovery/unreleased/search', { status: 200, json: fixture });

    const decoded = await searchUnreleasedTracks('cancun');

    expect(fixture.items.length).toBeGreaterThan(0);
    expect(decoded.items).toHaveLength(fixture.items.length);
    expect(decoded.items[0]!.artist.name).not.toBe('');
  });
});
