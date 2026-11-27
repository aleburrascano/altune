import {
  addTracksToPlaylist,
  backfillFeaturedArtists,
  createPlaylist,
  createTrack,
  fetchAudioUrls,
  getLibraryAlbums,
  getLibraryArtists,
  getPlaylist,
  getPlaylists,
  getTracks,
  listTracksFeaturing,
  parseTrackResponse,
  removeTracksFromPlaylist,
  renamePlaylist,
} from '@shared/apiClient';
import { asPlaylistId, asTrackId, type CreateTrackRequest } from '@shared/catalog';
import { signedInWith } from '@shared/apiClient/__tests__/sessionLookup';
import { wireFixture } from '@testing/wireFixtures';

const { __http } = require('@testing/doubles/fetch.js');

beforeEach(() => {
  signedInWith();
});

function asObject(value: unknown): Record<string, unknown> {
  return value as Record<string, unknown>;
}

function itemsOf(fixture: unknown): Record<string, unknown>[] {
  return asObject(fixture).items as Record<string, unknown>[];
}

function sortedKeys(value: unknown): string[] {
  return Object.keys(asObject(value)).sort();
}

function sentBody(): unknown {
  return JSON.parse(__http.last().body);
}

const TRACK_ID = asTrackId('0a0b0c0d-0000-4000-8000-000000000001');
const PLAYLIST_ID = asPlaylistId('0a0b0c0d-0000-4000-8000-000000000002');

describe('tracks wire fixtures from go-api decode through the client adapters', () => {
  it('decodes the track listing and carries the optional fields go-api sends', async () => {
    const fixture = wireFixture('tracks_list');
    __http.reply('GET /v1/tracks', { status: 200, json: fixture });

    const result = await getTracks({ limit: 50, offset: 0 });

    const sent = itemsOf(fixture)[0]!;
    expect(result.items).toHaveLength(itemsOf(fixture).length);
    expect(result.items[0]).toMatchObject({
      id: sent.id,
      title: sent.title,
      track_number: sent.track_number,
      year: sent.year,
      genre: sent.genre,
      isrc: sent.isrc,
      album_artist: sent.album_artist,
      artwork_url: sent.artwork_url,
      duration_seconds: sent.duration_seconds,
    });
    expect(result.items[0]!.featured_artists).toHaveLength(1);
    expect(typeof result.has_more).toBe('boolean');
  });

  it('never receives audio_ref on the wire, which go-api keeps server side', () => {
    const fixture = wireFixture('tracks_list');

    expect(itemsOf(fixture)[0]).not.toHaveProperty('audio_ref');
    expect(parseTrackResponse(itemsOf(fixture)[0]).audio_ref).toBeNull();
  });

  it('decodes the created track', async () => {
    const fixture = wireFixture('tracks_create');
    __http.reply('POST /v1/tracks', { status: 201, json: fixture });

    const created = await createTrack(wireFixtureAsCreateRequest());

    expect(created.title).toBe(asObject(fixture).title);
    expect(created.acquisition_status).toBe(asObject(fixture).acquisition_status);
  });

  it('decodes the featuring listing as the same envelope of tracks', async () => {
    __http.reply('GET /v1/tracks/featuring', {
      status: 200,
      json: wireFixture('tracks_featuring'),
    });

    const result = await listTracksFeaturing({
      name: 'Guest',
      mbid: '5b11f4ce-a62d-471e-81fc-a69a8278c7da',
      deezer_id: 42,
    });

    expect(result.items[0]!.featured_artists?.[0]?.name).toBe('Guest');
  });

  it('decodes the featured backfill progress', async () => {
    const fixture = asObject(wireFixture('tracks_featured_backfill'));
    __http.reply('POST /v1/tracks/featured-backfill', { status: 200, json: fixture });

    const result = await backfillFeaturedArtists();

    expect(result).toEqual({
      scanned: fixture.scanned,
      updated: fixture.updated,
      truncated: fixture.truncated,
      nextOffset: fixture.next_offset,
    });
  });
});

describe('audio url wire fixture', () => {
  it('decodes and drops expires_at on purpose', async () => {
    const fixture = asObject(wireFixture('audio_urls'));
    const sent = (fixture.urls as unknown[])[0];
    __http.reply('POST /v1/audio-urls', { status: 200, json: fixture });

    const result = await fetchAudioUrls([TRACK_ID]);

    expect(sortedKeys(sent)).toEqual(['expires_at', 'track_id', 'url', 'version']);
    expect(result).toHaveLength(1);
    expect(sortedKeys(result[0])).toEqual(['trackId', 'url', 'version']);
  });

  it('sends exactly the keys go-api accepts', async () => {
    __http.reply('POST /v1/audio-urls', { status: 200, json: wireFixture('audio_urls') });

    await fetchAudioUrls([TRACK_ID]);

    expect(sortedKeys(sentBody())).toEqual(sortedKeys(wireFixture('req_audio_urls')));
  });
});

describe('library lens wire fixtures decode through the client adapters', () => {
  it('decodes the album groups', async () => {
    const fixture = wireFixture('library_albums');
    __http.reply('GET /v1/library/albums', { status: 200, json: fixture });

    const result = await getLibraryAlbums();

    expect(sortedKeys(result.items[0])).toEqual(sortedKeys(itemsOf(fixture)[0]));
    expect(result.items[0]!.year).toBe(itemsOf(fixture)[0]!.year);
  });

  it('decodes the artist groups', async () => {
    const fixture = wireFixture('library_artists');
    __http.reply('GET /v1/library/artists', { status: 200, json: fixture });

    const result = await getLibraryArtists();

    expect(sortedKeys(result.items[0])).toEqual(sortedKeys(itemsOf(fixture)[0]));
  });
});

describe('playlist wire fixtures decode through the client adapters', () => {
  it('decodes the playlist listing with preview artwork', async () => {
    __http.reply('GET /v1/playlists', { status: 200, json: wireFixture('playlists_list') });

    const result = await getPlaylists();

    expect(result.items[0]!.preview_artwork_urls.length).toBeGreaterThan(0);
  });

  it('decodes the playlist detail with its tracks', async () => {
    const fixture = wireFixture('playlists_detail');
    __http.reply('GET /v1/playlists/' + PLAYLIST_ID, { status: 200, json: fixture });

    const result = await getPlaylist(PLAYLIST_ID);

    expect(result.tracks).toHaveLength(1);
    expect(typeof result.total_duration_seconds).toBe('number');
  });

  it('decodes the created and the renamed playlist', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: wireFixture('playlists_create') });
    __http.reply('PATCH /v1/playlists/' + PLAYLIST_ID, {
      status: 200,
      json: wireFixture('playlists_rename'),
    });

    const created = await createPlaylist({ name: 'Road trip' });
    const renamed = await renamePlaylist(PLAYLIST_ID, 'Night drive');

    expect(created.name).toBe('Road trip');
    expect(renamed.name).toBe('Night drive');
  });

  it('decodes the batch add and remove counts', async () => {
    __http.reply('POST /v1/playlists/' + PLAYLIST_ID + '/tracks/batch', {
      status: 200,
      json: wireFixture('playlists_add_tracks'),
    });
    __http.reply('DELETE /v1/playlists/' + PLAYLIST_ID + '/tracks', {
      status: 200,
      json: wireFixture('playlists_remove_tracks'),
    });

    const added = await addTracksToPlaylist(PLAYLIST_ID, { track_ids: [TRACK_ID] });
    const removed = await removeTracksFromPlaylist(PLAYLIST_ID, { track_ids: [TRACK_ID] });

    expect(added.added).toBe(1);
    expect(removed.removed).toBe(1);
  });
});

describe('request bodies the client sends have the keys go-api accepts', () => {
  it('sends the create track body with every key of the fixture, including track_number and featured artists', async () => {
    __http.reply('POST /v1/tracks', { status: 201, json: wireFixture('tracks_create') });
    const fixture = asObject(wireFixture('req_tracks_create'));

    await createTrack(wireFixtureAsCreateRequest());

    const sent = asObject(sentBody());
    expect(sortedKeys(sent)).toEqual(sortedKeys(fixture));
    expect(sortedKeys((sent.featured_artists as unknown[])[0])).toEqual(
      sortedKeys((fixture.featured_artists as unknown[])[0]),
    );
  });

  it('sends the playlist create, rename, add and remove bodies with the fixture keys', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: wireFixture('playlists_create') });
    __http.reply('PATCH /v1/playlists/' + PLAYLIST_ID, {
      status: 200,
      json: wireFixture('playlists_rename'),
    });
    __http.reply('POST /v1/playlists/' + PLAYLIST_ID + '/tracks/batch', {
      status: 200,
      json: wireFixture('playlists_add_tracks'),
    });
    __http.reply('DELETE /v1/playlists/' + PLAYLIST_ID + '/tracks', {
      status: 200,
      json: wireFixture('playlists_remove_tracks'),
    });

    await createPlaylist({ name: 'Road trip' });
    const create = sortedKeys(sentBody());
    await renamePlaylist(PLAYLIST_ID, 'Night drive');
    const rename = sortedKeys(sentBody());
    await addTracksToPlaylist(PLAYLIST_ID, { track_ids: [TRACK_ID] });
    const add = sortedKeys(sentBody());
    await removeTracksFromPlaylist(PLAYLIST_ID, { track_ids: [TRACK_ID] });
    const remove = sortedKeys(sentBody());

    expect(create).toEqual(sortedKeys(wireFixture('req_playlists_create')));
    expect(rename).toEqual(sortedKeys(wireFixture('req_playlists_rename')));
    expect(add).toEqual(sortedKeys(wireFixture('req_playlists_add_tracks')));
    expect(remove).toEqual(sortedKeys(wireFixture('req_playlists_remove_tracks')));
  });
});

function wireFixtureAsCreateRequest(): CreateTrackRequest {
  return {
    title: 'Song',
    artist: 'Artist',
    album: 'Album',
    duration_seconds: 215.5,
    artwork_url: 'https://example.com/art.jpg',
    isrc: 'USRC17607839',
    year: 2020,
    genre: 'Rock',
    album_artist: 'Artist',
    track_number: 3,
    featured_artists: [{ name: 'Guest', mbid: 'm', deezer_id: 42 }],
    source_url: 'https://soundcloud.com/artist/song',
    mbid: 'm',
    explicit: false,
    sources: [{ provider: 'deezer', external_id: '12345' }],
  };
}
