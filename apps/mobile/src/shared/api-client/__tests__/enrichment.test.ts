import {
  getAlbumTracks,
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
  provider_name: 'musicbrainz',
  status: 'ok',
};

// What each enrichment endpoint answers for an entity it found nothing for: a
// fully-populated payload flagged has_content:false, never a partial body.
const emptyEnrichment = {
  has_content: false,
  mbid: '',
  genres: [],
  year: 0,
  rating: 0,
  rating_votes: 0,
  primary_type: '',
  secondary_types: [],
  external_ids: {},
  artwork_url: '',
};

const emptyLastFm = {
  has_content: false,
  mbid: '',
  listeners: 0,
  playcount: 0,
  tags: [],
  bio: '',
  similar: [],
  duration: 0,
  album: '',
};

const emptyDeezer = {
  has_content: false,
  bpm: 0,
  gain: 0,
  explicit: false,
  label: '',
  genres: [],
  upc: '',
  record_type: '',
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

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', limit: undefined });
    expect(__http.last().query).not.toContain('limit');

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', limit: 0 });
    expect(__http.last().query).toContain('limit=0');
  });

  it('drops an empty-string albumTitle exactly like an absent one (truthiness guard)', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', albumTitle: '' });
    expect(__http.last().url).not.toContain('title=');

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', albumTitle: undefined });
    expect(__http.last().url).not.toContain('title=');
  });

  it('includes albumTitle, albumArtist and mbExternalId when each is a real string', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks({
      provider: 'musicbrainz',
      externalId: 'mb-1',
      limit: 5,
      albumTitle: 'Rumours',
      albumArtist: 'Fleetwood Mac',
      mbExternalId: 'mb-album-2',
    });

    expect(__http.last().query).toBe('limit=5&title=Rumours&artist=Fleetwood+Mac&mbid=mb-album-2');
  });

  it('drops albumArtist when absent or empty, and includes it only when a real string is given', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', albumArtist: undefined });
    expect(new URLSearchParams(__http.last().query).has('artist')).toBe(false);

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', albumArtist: '' });
    expect(new URLSearchParams(__http.last().query).has('artist')).toBe(false);

    await getAlbumTracks({
      provider: 'musicbrainz',
      externalId: 'mb-1',
      albumArtist: 'Fleetwood Mac',
    });
    expect(new URLSearchParams(__http.last().query).get('artist')).toBe('Fleetwood Mac');
  });

  it('drops mbExternalId when absent or empty, and includes it only when a real string is given', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', mbExternalId: undefined });
    expect(new URLSearchParams(__http.last().query).has('mbid')).toBe(false);

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', mbExternalId: '' });
    expect(new URLSearchParams(__http.last().query).has('mbid')).toBe(false);

    await getAlbumTracks({
      provider: 'musicbrainz',
      externalId: 'mb-1',
      mbExternalId: 'mb-album-2',
    });
    expect(new URLSearchParams(__http.last().query).get('mbid')).toBe('mb-album-2');
  });

  it('encodeURIComponent-encodes externalId so a slash/question-mark cannot escape its path segment', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb%2F1%3F/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb/1?' });

    expect(__http.last().path).toBe('/v1/discovery/albums/musicbrainz/mb%2F1%3F/tracks');
  });

  it('encodeURIComponent-encodes provider too, so a slash cannot escape its own path segment', async () => {
    __http.reply('GET /v1/discovery/albums/evil%2Fother%2Fhijacked/mb-1/tracks', {
      status: 200,
      json: emptyContent,
    });

    await getAlbumTracks({ provider: 'evil/other/hijacked', externalId: 'mb-1' });

    expect(__http.last().path).toBe('/v1/discovery/albums/evil%2Fother%2Fhijacked/mb-1/tracks');
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
    const topTracks: ContentFetchResponse = { ...emptyContent, provider_name: 'deezer' };
    const albums: ContentFetchResponse = { ...emptyContent, provider_name: 'deezer', status: 'timeout' };
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
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });

    await getEnrichment({ kind: 'artist' });

    expect(__http.last().query).toBe('kind=artist');
  });

  it('omits subtitle when it is null (not the same code path as undefined, same wire effect)', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });

    await getEnrichment({ kind: 'album', title: 'Rumours', subtitle: null });

    expect(__http.last().query).toBe('kind=album&title=Rumours');
  });

  it('omits subtitle when it is an empty string (truthiness guard, same as null/undefined)', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });

    await getEnrichment({ kind: 'album', title: 'Rumours', subtitle: '' });

    expect(__http.last().query).toBe('kind=album&title=Rumours');
  });

  it('includes subtitle when it is a real string, for album/track kinds', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });

    await getEnrichment({ kind: 'track', title: 'Dreams', subtitle: 'Fleetwood Mac' });

    expect(__http.last().query).toBe('kind=track&title=Dreams&subtitle=Fleetwood+Mac');
  });

  it('sends only kind and mbid when mbid is present alone', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });

    await getEnrichment({ kind: 'track', mbid: 'mb-track-1' });

    expect(__http.last().query).toBe('kind=track&mbid=mb-track-1');
  });

  it('includes mbid alongside title and subtitle, dropping nothing when the precise identifier and the fuzzy ones coexist', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });

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
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });

    await getEnrichment({ kind: 'track', title: 'Dreams', mbid: '' });

    expect(__http.last().query).toBe('kind=track&title=Dreams');
  });
});

describe('kindTitleQs shared by getLastFmEnrichment / getDeezerEnrichment', () => {
  it('getLastFmEnrichment omits subtitle when null, includes it when a real string', async () => {
    __http.reply('GET /v1/discovery/enrichment/lastfm', { status: 200, json: emptyLastFm });

    await getLastFmEnrichment({ kind: 'artist', title: 'Fleetwood Mac', subtitle: null });
    expect(__http.last().query).toBe('kind=artist&title=Fleetwood+Mac');

    await getLastFmEnrichment({ kind: 'track', title: 'Dreams', subtitle: 'Fleetwood Mac' });
    expect(__http.last().query).toBe('kind=track&title=Dreams&subtitle=Fleetwood+Mac');
  });

  it('getDeezerEnrichment omits subtitle when undefined, includes it when a real string', async () => {
    __http.reply('GET /v1/discovery/enrichment/deezer', { status: 200, json: emptyDeezer });

    await getDeezerEnrichment({ kind: 'artist', title: 'Fleetwood Mac' });
    expect(__http.last().query).toBe('kind=artist&title=Fleetwood+Mac');

    await getDeezerEnrichment({ kind: 'album', title: 'Rumours', subtitle: 'Fleetwood Mac' });
    expect(__http.last().query).toBe('kind=album&title=Rumours&subtitle=Fleetwood+Mac');
  });
});

describe('enrichment responses violating the null-object contract', () => {
  it('getEnrichment resolves the empty payload as-is (caller branches on has_content, not on a thrown error)', async () => {
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });

    const result = await getEnrichment({ kind: 'artist', title: 'Unknown Artist' });

    expect(result).toEqual(emptyEnrichment);
  });

  it('getEnrichment keeps a populated genres list, and names the off-contract member when one is not a string', async () => {
    __http.replyOnce('GET /v1/discovery/enrichment', {
      status: 200,
      json: { ...emptyEnrichment, has_content: true, genres: ['rock', 'folk'] },
    });
    const populated = await getEnrichment({ kind: 'album', title: 'Rumours' });

    __http.replyOnce('GET /v1/discovery/enrichment', {
      status: 200,
      json: { ...emptyEnrichment, genres: ['rock', 7] },
    });
    const rejection = await getEnrichment({ kind: 'album', title: 'Rumours' }).catch(
      (error: unknown) => error,
    );

    expect(populated.genres).toEqual(['rock', 'folk']);
    expect(rejection).toMatchObject({ name: 'ContractError', at: 'EnrichmentResponse.genres[1]' });
  });

  it('getEnrichment keeps the external_ids map of a populated response', async () => {
    __http.reply('GET /v1/discovery/enrichment', {
      status: 200,
      json: { ...emptyEnrichment, has_content: true, external_ids: { discogs: '123' } },
    });

    const result = await getEnrichment({ kind: 'album', title: 'Rumours' });

    expect(result.external_ids).toEqual({ discogs: '123' });
  });

  it.each([
    ['genres is missing', { ...emptyEnrichment, genres: undefined }, 'genres'],
    ['year is a string', { ...emptyEnrichment, year: '1977' }, 'year'],
    ['has_content is missing', { ...emptyEnrichment, has_content: undefined }, 'has_content'],
    [
      'an external_ids value is not a string',
      { ...emptyEnrichment, external_ids: { discogs: 123 } },
      'external_ids.discogs',
    ],
  ])(
    'getEnrichment rejects with a ContractError naming the field when %s',
    async (_label, json, field) => {
      __http.reply('GET /v1/discovery/enrichment', { status: 200, json });

      await expect(getEnrichment({ kind: 'artist', title: 'Fleetwood Mac' })).rejects.toMatchObject(
        { name: 'ContractError', at: `EnrichmentResponse.${field}` },
      );
    },
  );

  it('getDeezerEnrichment rejects a response missing the "always present" genres collection', async () => {
    __http.reply('GET /v1/discovery/enrichment/deezer', {
      status: 200,
      json: { ...emptyDeezer, has_content: true, bpm: 120, upc: '123', genres: undefined },
    });

    await expect(getDeezerEnrichment({ kind: 'track', title: 'Dreams' })).rejects.toMatchObject({
      name: 'ContractError',
      at: 'DeezerEnrichmentResponse.genres',
    });
  });

  it('getDeezerEnrichment keeps featured_artists when the wire carries them, and omits the key when it does not', async () => {
    __http.replyOnce('GET /v1/discovery/enrichment/deezer', {
      status: 200,
      json: { ...emptyDeezer, featured_artists: [{ name: 'Travis Scott' }] },
    });
    const withFeatured = await getDeezerEnrichment({ kind: 'track', title: 'No Idea' });

    __http.replyOnce('GET /v1/discovery/enrichment/deezer', { status: 200, json: emptyDeezer });
    const withoutFeatured = await getDeezerEnrichment({ kind: 'track', title: 'No Idea' });

    expect(withFeatured.featured_artists).toEqual([{ name: 'Travis Scott' }]);
    expect(withoutFeatured).not.toHaveProperty('featured_artists');
  });

  it('getLastFmEnrichment rejects a response whose tags collection is null', async () => {
    __http.reply('GET /v1/discovery/enrichment/lastfm', {
      status: 200,
      json: { ...emptyLastFm, tags: null },
    });

    await expect(
      getLastFmEnrichment({ kind: 'artist', title: 'Fleetwood Mac' }),
    ).rejects.toMatchObject({ name: 'ContractError', at: 'LastFmEnrichmentResponse.tags' });
  });
});

describe('content fetch responses are parsed, not cast', () => {
  const trackItem = {
    kind: 'track',
    title: 'Dreams',
    subtitle: 'Fleetwood Mac',
    confidence: 'high',
    sources: [{ provider: 'deezer', external_id: 'd-1', url: 'https://deezer/1' }],
    extras: {},
  };

  it('resolves the items of a well-formed album-tracks response, nulling the fields the wire omits', async () => {
    __http.reply('GET /v1/discovery/albums/musicbrainz/mb-1/tracks', {
      status: 200,
      json: { ...emptyContent, items: [trackItem] },
    });

    const result = await getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1' });

    expect(result.items).toEqual([
      {
        kind: 'track',
        title: 'Dreams',
        subtitle: 'Fleetwood Mac',
        image_url: null,
        confidence: 'high',
        sources: [{ provider: 'deezer', external_id: 'd-1', url: 'https://deezer/1' }],
        extras: {},
      },
    ]);
  });

  it.each([
    ['items is missing', { ...emptyContent, items: undefined }, 'ContentFetchResponse.items'],
    [
      'an item drifts from the contract',
      { ...emptyContent, items: [{ ...trackItem, title: 42 }] },
      'ContentFetchResponse.items[0].title',
    ],
    [
      'status is not a provider status',
      { ...emptyContent, status: 'degraded' },
      'ContentFetchResponse.status',
    ],
    [
      'provider_name is missing',
      { ...emptyContent, provider_name: undefined },
      'ContentFetchResponse.provider_name',
    ],
  ])('rejects with a ContractError when %s', async (_label, json, at) => {
    __http.reply('GET /v1/discovery/tracks/soundcloud/sc-1/related', { status: 200, json });

    await expect(getRelatedTracks('soundcloud', 'sc-1')).rejects.toMatchObject({
      name: 'ContractError',
      at,
    });
  });

  it('getArtistContent names which half of the pair broke the contract', async () => {
    __http.reply('GET /v1/discovery/artists/deezer/art-1/content', {
      status: 200,
      json: { top_tracks: emptyContent, albums: { ...emptyContent, items: null } },
    });

    await expect(getArtistContent('deezer', 'art-1')).rejects.toMatchObject({
      name: 'ContractError',
      at: 'ArtistContentResponse.albums.items',
    });
  });
});
