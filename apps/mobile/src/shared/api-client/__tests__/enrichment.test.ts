import {
  getAlbumTracks,
  getArtistTopTracks,
  getArtistAlbums,
  getRelatedTracks,
  getArtistContent,
  getEnrichment,
  getLastFmEnrichment,
  getDeezerEnrichment,
  type ContentFetchResponse,
} from '../enrichment';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const emptyContent: ContentFetchResponse = {
  items: [],
  provider: 'musicbrainz',
  status: 'ok',
  latency_ms: 12,
};

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

describe('getAlbumTracks', () => {
  it('omits limit when undefined but sends limit=0 when explicitly zero', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks('musicbrainz', 'mb-1', undefined);
    expect(__http.last().query).not.toContain('limit');

    await getAlbumTracks('musicbrainz', 'mb-1', 0);
    expect(__http.last().query).toContain('limit=0');
  });

  it('drops an empty-string albumTitle exactly like an absent one (truthiness guard)', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks('musicbrainz', 'mb-1', undefined, '');
    expect(__http.last().url).not.toContain('title=');

    await getAlbumTracks('musicbrainz', 'mb-1', undefined, undefined);
    expect(__http.last().url).not.toContain('title=');
  });

  it('includes albumTitle, albumArtist and mbExternalId when each is a real string', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks('musicbrainz', 'mb-1', 5, 'Rumours', 'Fleetwood Mac', 'mb-album-2');

    expect(__http.last().query).toBe('limit=5&title=Rumours&artist=Fleetwood+Mac&mbid=mb-album-2');
  });

  it('drops albumArtist when absent or empty, and includes it only when a real string is given', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks('musicbrainz', 'mb-1', undefined, undefined, undefined);
    expect(new URLSearchParams(__http.last().query).has('artist')).toBe(false);

    await getAlbumTracks('musicbrainz', 'mb-1', undefined, undefined, '');
    expect(new URLSearchParams(__http.last().query).has('artist')).toBe(false);

    await getAlbumTracks('musicbrainz', 'mb-1', undefined, undefined, 'Fleetwood Mac');
    expect(new URLSearchParams(__http.last().query).get('artist')).toBe('Fleetwood Mac');
  });

  it('drops mbExternalId when absent or empty, and includes it only when a real string is given', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks('musicbrainz', 'mb-1', undefined, undefined, undefined, undefined);
    expect(new URLSearchParams(__http.last().query).has('mbid')).toBe(false);

    await getAlbumTracks('musicbrainz', 'mb-1', undefined, undefined, undefined, '');
    expect(new URLSearchParams(__http.last().query).has('mbid')).toBe(false);

    await getAlbumTracks('musicbrainz', 'mb-1', undefined, undefined, undefined, 'mb-album-2');
    expect(new URLSearchParams(__http.last().query).get('mbid')).toBe('mb-album-2');
  });

  it('encodeURIComponent-encodes externalId so a slash/question-mark cannot escape its path segment', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb%2F1%3F/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks('musicbrainz', 'mb/1?');

    expect(__http.last().path).toBe('/v1/discovery/albums/musicbrainz/mb%2F1%3F/tracks');
  });

  it('encodeURIComponent-encodes provider too, so a slash cannot escape its own path segment', async () => {
    __http.reply('GET /v1/discovery/albums/evil%2Fother%2Fhijacked/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks('evil/other/hijacked', 'mb-1');

    expect(__http.last().path).toBe('/v1/discovery/albums/evil%2Fother%2Fhijacked/mb-1/tracks');
  });
});

describe('getArtistTopTracks (dead code, zero call sites — still exported)', () => {
  it('omits limit and name when absent', async () => {
    __http.reply('GET /v1/discovery/artists/deezer/art-1/top-tracks', {
      status: 200,
      json: emptyContent,
    });

    await getArtistTopTracks('deezer', 'art-1');

    expect(__http.last().query).toBe('');
  });

  it('includes limit and artistName when present', async () => {
    __http.reply('GET /v1/discovery/artists/deezer/art-1/top-tracks', {
      status: 200,
      json: emptyContent,
    });

    await getArtistTopTracks('deezer', 'art-1', 10, 'Daft Punk');

    expect(__http.last().query).toBe('limit=10&name=Daft+Punk');
  });
});

describe('getArtistAlbums (dead code, zero call sites — still exported)', () => {
  it('omits limit and name when absent', async () => {
    __http.reply('GET /v1/discovery/artists/deezer/art-1/albums', {
      status: 200,
      json: emptyContent,
    });

    await getArtistAlbums('deezer', 'art-1');

    expect(__http.last().query).toBe('');
  });

  it('includes limit and artistName when present', async () => {
    __http.reply('GET /v1/discovery/artists/deezer/art-1/albums', {
      status: 200,
      json: emptyContent,
    });

    await getArtistAlbums('deezer', 'art-1', 3, 'Daft Punk');

    expect(__http.last().query).toBe('limit=3&name=Daft+Punk');
  });
});

describe('getRelatedTracks (string-concatenation query builder, _contentUrl)', () => {
  it('produces a bare URL with no "?" at all when limit is undefined', async () => {
    __http.reply('GET /v1/discovery/tracks/soundcloud/sc-1/related', {
      status: 200,
      json: emptyContent,
    });

    await getRelatedTracks('soundcloud', 'sc-1');

    expect(__http.last().url).not.toContain('?');
  });

  it('appends exactly ?limit=<n> when limit is provided, matching the only production call shape', async () => {
    __http.reply('GET /v1/discovery/tracks/soundcloud/sc-1/related', {
      status: 200,
      json: emptyContent,
    });

    await getRelatedTracks('soundcloud', 'sc-1', 20);

    expect(__http.last().url.endsWith('/related?limit=20')).toBe(true);
    expect(__http.last().query).toBe('limit=20');
  });

  it('encodeURIComponent-encodes provider, so a path-traversal-shaped value stays inside its own segment', async () => {
    __http.reply('GET /v1/discovery/tracks/a%2F..%2Fb/sc-1/related', {
      status: 200,
      json: emptyContent,
    });

    await getRelatedTracks('a/../b', 'sc-1');

    expect(__http.last().path).toBe('/v1/discovery/tracks/a%2F..%2Fb/sc-1/related');
  });

  it('encodeURIComponent-encodes a space and a hash in provider, keeping the request addressed to /related', async () => {
    __http.reply('GET /v1/discovery/tracks/sc%20provider%23x/sc-1/related', {
      status: 200,
      json: emptyContent,
    });

    await getRelatedTracks('sc provider#x', 'sc-1');

    expect(__http.last().path).toBe('/v1/discovery/tracks/sc%20provider%23x/sc-1/related');
  });
});

describe('getArtistContent', () => {
  it('sends no query string when opts is omitted entirely', async () => {
    __http.reply('GET /v1/discovery/artists/deezer/art-1/content', {
      status: 200,
      json: { top_tracks: emptyContent, albums: emptyContent },
    });

    await getArtistContent('deezer', 'art-1');

    expect(__http.last().query).toBe('');
  });

  it('sends name, tracks_limit and albums_limit together in that order when all present', async () => {
    __http.reply('GET /v1/discovery/artists/deezer/art-1/content', {
      status: 200,
      json: { top_tracks: emptyContent, albums: emptyContent },
    });

    await getArtistContent('deezer', 'art-1', {
      artistName: 'Daft Punk',
      tracksLimit: 5,
      albumsLimit: 8,
    });

    expect(__http.last().query).toBe('name=Daft+Punk&tracks_limit=5&albums_limit=8');
  });

  it('sends tracks_limit=0 and albums_limit=0 rather than dropping them (0 must not fall back to the server default)', async () => {
    __http.reply('GET /v1/discovery/artists/deezer/art-1/content', {
      status: 200,
      json: { top_tracks: emptyContent, albums: emptyContent },
    });

    await getArtistContent('deezer', 'art-1', { tracksLimit: 0, albumsLimit: 0 });

    expect(__http.last().query).toBe('tracks_limit=0&albums_limit=0');
  });

  it('returns the top_tracks and albums sections as received', async () => {
    const topTracks: ContentFetchResponse = { ...emptyContent, provider: 'deezer', latency_ms: 42 };
    const albums: ContentFetchResponse = { ...emptyContent, provider: 'deezer', latency_ms: 7 };
    __http.reply('GET /v1/discovery/artists/deezer/art-1/content', {
      status: 200,
      json: { top_tracks: topTracks, albums },
    });

    const result = await getArtistContent('deezer', 'art-1');

    expect(result).toEqual({ top_tracks: topTracks, albums });
  });
});

describe('getEnrichment (MusicBrainz) — title optional, subtitle nullable', () => {
  it('sends only kind when title, subtitle and mbid are all absent', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: {} });

    await getEnrichment({ kind: 'artist' });

    expect(__http.last().query).toBe('kind=artist');
  });

  it('omits subtitle when it is null (not the same code path as undefined, same wire effect)', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: {} });

    await getEnrichment({ kind: 'album', title: 'Rumours', subtitle: null });

    expect(__http.last().query).toBe('kind=album&title=Rumours');
  });

  it('omits subtitle when it is an empty string (truthiness guard, same as null/undefined)', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: {} });

    await getEnrichment({ kind: 'album', title: 'Rumours', subtitle: '' });

    expect(__http.last().query).toBe('kind=album&title=Rumours');
  });

  it('includes subtitle when it is a real string, for album/track kinds', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: {} });

    await getEnrichment({ kind: 'track', title: 'Dreams', subtitle: 'Fleetwood Mac' });

    expect(__http.last().query).toBe('kind=track&title=Dreams&subtitle=Fleetwood+Mac');
  });

  it('sends only kind and mbid when mbid is present alone', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: {} });

    await getEnrichment({ kind: 'track', mbid: 'mb-track-1' });

    expect(__http.last().query).toBe('kind=track&mbid=mb-track-1');
  });

  it('includes mbid alongside title and subtitle, dropping nothing when the precise identifier and the fuzzy ones coexist', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: {} });

    await getEnrichment({
      kind: 'track',
      title: 'Dreams',
      subtitle: 'Fleetwood Mac',
      mbid: 'mb-track-1',
    });

    expect(__http.last().query).toBe(
      'kind=track&title=Dreams&subtitle=Fleetwood+Mac&mbid=mb-track-1',
    );
  });

  it('drops mbid when it is an empty string (truthiness guard, same as its siblings)', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: {} });

    await getEnrichment({ kind: 'track', title: 'Dreams', mbid: '' });

    expect(__http.last().query).toBe('kind=track&title=Dreams');
  });
});

describe('kindTitleQs shared by getLastFmEnrichment / getDeezerEnrichment', () => {
  it('getLastFmEnrichment omits subtitle when null, includes it when a real string', async () => {
    __http.reply('GET /v1/discovery/enrichment/lastfm', { status: 200, json: {} });

    await getLastFmEnrichment({ kind: 'artist', title: 'Fleetwood Mac', subtitle: null });
    expect(__http.last().query).toBe('kind=artist&title=Fleetwood+Mac');

    await getLastFmEnrichment({ kind: 'track', title: 'Dreams', subtitle: 'Fleetwood Mac' });
    expect(__http.last().query).toBe('kind=track&title=Dreams&subtitle=Fleetwood+Mac');
  });

  it('getDeezerEnrichment omits subtitle when undefined, includes it when a real string', async () => {
    __http.reply('GET /v1/discovery/enrichment/deezer', { status: 200, json: {} });

    await getDeezerEnrichment({ kind: 'artist', title: 'Fleetwood Mac' });
    expect(__http.last().query).toBe('kind=artist&title=Fleetwood+Mac');

    await getDeezerEnrichment({ kind: 'album', title: 'Rumours', subtitle: 'Fleetwood Mac' });
    expect(__http.last().query).toBe('kind=album&title=Rumours&subtitle=Fleetwood+Mac');
  });
});

describe('enrichment responses violating the null-object contract', () => {
  it('getEnrichment returns has_content:false payload as-is (caller branches on has_content, not on a thrown error)', async () => {
    __http.reply('GET /v1/discovery/enrichment', {
      status: 200,
      json: { has_content: false, mbid: '', genres: [], external_ids: {} },
    });

    const result = await getEnrichment({ kind: 'artist', title: 'Unknown Artist' });

    expect(result).toEqual({
      has_content: false,
      mbid: '',
      genres: [],
      external_ids: {},
    });
  });

  it('getDeezerEnrichment passes through a response missing the "always present" genres collection unmodified', async () => {
    __http.reply('GET /v1/discovery/enrichment/deezer', {
      status: 200,
      json: { has_content: true, bpm: 120, upc: '123' },
    });

    const result = await getDeezerEnrichment({ kind: 'track', title: 'Dreams' });

    expect((result as Record<string, unknown>).genres).toBeUndefined();
  });
});
