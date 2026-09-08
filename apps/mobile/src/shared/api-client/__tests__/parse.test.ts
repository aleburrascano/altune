import { ContractError, isRetryable } from '../errors';
import {
  asArray,
  asBoolean,
  asNumber,
  asRecord,
  asString,
  member,
  nullableNumber,
  nullableString,
  parseDiscoverySearchResponse,
  parseListAlbumsResponse,
  parseListArtistsResponse,
  parseListTracksResponse,
  parseTrackResponse,
} from '../parse';

describe('primitive narrowers return the value or throw a ContractError', () => {
  it('asRecord accepts a plain object and rejects a number, null, and an array', () => {
    expect(asRecord({ a: 1 }, 'r')).toEqual({ a: 1 });
    expect(() => asRecord(42, 'r')).toThrow(ContractError);
    expect(() => asRecord(null, 'r')).toThrow(ContractError);
    expect(() => asRecord([], 'r')).toThrow(ContractError);
  });

  it('asArray accepts an array and rejects an object', () => {
    expect(asArray([1, 2], 'a')).toEqual([1, 2]);
    expect(() => asArray({}, 'a')).toThrow(ContractError);
  });

  it('asString accepts a string and rejects a number', () => {
    expect(asString('x', 's')).toBe('x');
    expect(() => asString(5, 's')).toThrow(ContractError);
  });

  it('asNumber accepts a number and rejects a string', () => {
    expect(asNumber(5, 'n')).toBe(5);
    expect(() => asNumber('x', 'n')).toThrow(ContractError);
  });

  it('asBoolean accepts a boolean and rejects a string', () => {
    expect(asBoolean(true, 'b')).toBe(true);
    expect(() => asBoolean('x', 'b')).toThrow(ContractError);
  });

  it('nullableString maps null and undefined to null, carries a string, and rejects a number', () => {
    expect(nullableString(null, 'ns')).toBeNull();
    expect(nullableString(undefined, 'ns')).toBeNull();
    expect(nullableString('x', 'ns')).toBe('x');
    expect(() => nullableString(5, 'ns')).toThrow(ContractError);
  });

  it('nullableNumber maps null and undefined to null, carries a number, and rejects a string', () => {
    expect(nullableNumber(null, 'nn')).toBeNull();
    expect(nullableNumber(undefined, 'nn')).toBeNull();
    expect(nullableNumber(7, 'nn')).toBe(7);
    expect(() => nullableNumber('x', 'nn')).toThrow(ContractError);
  });

  it('member accepts a declared value, rejects an undeclared one, and rejects a non-string', () => {
    expect(member('ready', ['pending', 'ready', 'failed'] as const, 'm')).toBe('ready');
    expect(() => member('nope', ['pending', 'ready', 'failed'] as const, 'm')).toThrow(
      ContractError,
    );
    expect(() => member(5, ['pending', 'ready', 'failed'] as const, 'm')).toThrow(ContractError);
  });

  it('classifies a ContractError as non-retryable (an off-contract body will not heal on retry)', () => {
    expect(isRetryable(new ContractError('x', 'y'))).toBe(false);
  });
});

function fullTrack(): Record<string, unknown> {
  return {
    id: 't1',
    title: 'Kid A',
    artist: 'Radiohead',
    album: null,
    duration_seconds: null,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

describe('parseTrackResponse', () => {
  it('parses a track without the optional failure_message or featured_artists', () => {
    const track = parseTrackResponse(fullTrack());
    expect(track.id).toBe('t1');
    expect(track).not.toHaveProperty('failure_message');
    expect(track).not.toHaveProperty('featured_artists');
  });

  it('parses the optional failure_message and featured_artists when present', () => {
    const track = parseTrackResponse({
      ...fullTrack(),
      failure_message: 'download failed',
      featured_artists: [{ name: 'Thom Yorke', mbid: 'mb-1', deezer_id: 9 }],
    });
    expect(track.failure_message).toBe('download failed');
    expect(track.featured_artists).toEqual([{ name: 'Thom Yorke', mbid: 'mb-1', deezer_id: 9 }]);
  });

  it('rejects an off-contract acquisition_status as a ContractError', () => {
    expect(() => parseTrackResponse({ ...fullTrack(), acquisition_status: 'weird' })).toThrow(
      ContractError,
    );
  });
});

describe('parseListTracksResponse', () => {
  it('parses a page and maps each item through the track parser', () => {
    const page = parseListTracksResponse({
      items: [fullTrack()],
      total: 1,
      limit: 20,
      offset: 0,
      has_more: false,
    });
    expect(page.items).toHaveLength(1);
    expect(page.total).toBe(1);
  });

  it('rejects a body whose items field is not an array', () => {
    expect(() =>
      parseListTracksResponse({ items: 'nope', total: 0, limit: 0, offset: 0, has_more: false }),
    ).toThrow(ContractError);
  });
});

describe('parseListAlbumsResponse and parseListArtistsResponse', () => {
  it('parses an album group lens', () => {
    const albums = parseListAlbumsResponse({
      items: [
        {
          key: 'a1',
          album: 'OK Computer',
          artist: 'Radiohead',
          artwork_url: 'https://img/ok.png',
          year: 1997,
          track_count: 12,
          most_recent_added_at: '2024-01-01T00:00:00Z',
        },
      ],
      total: 1,
    });
    expect(albums.items[0]!.album).toBe('OK Computer');
  });

  it('parses an artist group lens', () => {
    const artists = parseListArtistsResponse({
      items: [
        {
          key: 'ar1',
          artist: 'Radiohead',
          artwork_url: null,
          track_count: 40,
          most_recent_added_at: '2024-01-01T00:00:00Z',
        },
      ],
      total: 1,
    });
    expect(artists.items[0]!.artist).toBe('Radiohead');
  });
});

function fullResult(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    kind: 'track',
    title: 'Reckoner',
    subtitle: 'Radiohead',
    image_url: 'https://img/reckoner.png',
    confidence: 'high',
    sources: [{ provider: 'musicbrainz', external_id: 'mb-1', url: 'https://mb/1' }],
    extras: {},
    result_signature: 'sig-1',
    favorite_key: 'fav-1',
    ...overrides,
  };
}

describe('parseDiscoverySearchResponse — the full, structured shape', () => {
  const parsed = parseDiscoverySearchResponse({
    query: 'radiohead',
    query_norm: 'radiohead',
    results: [fullResult()],
    sections: [{ kind: 'album', items: [fullResult()], has_more: true }],
    providers: [{ provider: 'musicbrainz', status: 'ok', result_count: 3, latency_ms: 12 }],
    partial: false,
    cache: { hit: true, fetched_at: '2024-01-01T00:00:00Z' },
    total: 500,
    offset: 20,
    has_more: true,
    search_id: 'srch-1',
    top_result: fullResult({ kind: 'artist', title: 'Radiohead' }),
    corrected_query: 'radiohead',
    original_query: 'radiohed',
    related: [
      { relationship: 'similar', related_to: 'Radiohead', items: [fullResult({ kind: 'artist' })] },
    ],
  });

  it('carries the structured optional fields through', () => {
    expect(parsed.search_id).toBe('srch-1');
    expect(parsed.corrected_query).toBe('radiohead');
    expect(parsed.original_query).toBe('radiohed');
    expect(parsed.top_result!.title).toBe('Radiohead');
    expect(parsed.related![0]!.relationship).toBe('similar');
  });

  it('parses the provider list, cache, and per-result signature/favorite_key', () => {
    expect(parsed.providers[0]!.status).toBe('ok');
    expect(parsed.providers[0]!.result_count).toBe(3);
    expect(parsed.cache).toEqual({ hit: true, fetched_at: '2024-01-01T00:00:00Z' });
    expect(parsed.results[0]!.result_signature).toBe('sig-1');
    expect(parsed.results[0]!.favorite_key).toBe('fav-1');
    expect(parsed.sections[0]!.items).toHaveLength(1);
  });
});

describe('parseDiscoverySearchResponse — legacy server omitting newer fields', () => {
  const parsed = parseDiscoverySearchResponse({
    query: 'q',
    query_norm: 'q',
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
  });

  it('defaults results, sections, offset, has_more, and total (to results.length) rather than throwing', () => {
    expect(parsed.results).toEqual([]);
    expect(parsed.sections).toEqual([]);
    expect(parsed.offset).toBe(0);
    expect(parsed.has_more).toBe(false);
    expect(parsed.total).toBe(0);
  });

  it('omits the structured optional fields entirely', () => {
    expect(parsed).not.toHaveProperty('search_id');
    expect(parsed).not.toHaveProperty('top_result');
    expect(parsed).not.toHaveProperty('corrected_query');
    expect(parsed).not.toHaveProperty('original_query');
    expect(parsed).not.toHaveProperty('related');
  });
});

describe('parseDiscoverySearchResponse — off-contract bodies fail as a ContractError', () => {
  function base(): Record<string, unknown> {
    return {
      query: 'q',
      query_norm: 'q',
      results: [],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 0,
      offset: 0,
      has_more: false,
    };
  }

  it('rejects a null body', () => {
    expect(() => parseDiscoverySearchResponse(null)).toThrow(ContractError);
  });

  it('rejects an off-contract result confidence', () => {
    expect(() =>
      parseDiscoverySearchResponse({ ...base(), results: [fullResult({ confidence: 'extreme' })] }),
    ).toThrow(ContractError);
  });

  it('rejects an off-contract result kind', () => {
    expect(() =>
      parseDiscoverySearchResponse({ ...base(), results: [fullResult({ kind: 'playlist' })] }),
    ).toThrow(ContractError);
  });

  it('rejects an off-contract provider status', () => {
    expect(() =>
      parseDiscoverySearchResponse({
        ...base(),
        providers: [{ provider: 'mb', status: 'exploded', result_count: 0, latency_ms: 1 }],
      }),
    ).toThrow(ContractError);
  });
});
