import { ContractError } from '@shared/failure';
import {
  asFavoriteKey,
  asPlaylistId,
  asTrackId,
  idPathSegment,
  NO_PLAYLIST_ID,
  parsePlaylistId,
  parseTrackId,
  sourceIdList,
  type PlaylistId,
  type TrackId,
} from '@shared/catalog';

const UUID = '0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a';
const HOSTILE = ['', '.', '..', '../x', 'a/b', 'a?b=1', 'a#frag', 'a%2Fb', 'a b', 'x'.repeat(129)];
const RESERVED_OBJECT_KEYS = ['__proto__', 'constructor', 'prototype'];

describe.each([
  ['asTrackId', asTrackId],
  ['asPlaylistId', asPlaylistId],
] as const)('%s', (_name, brand) => {
  it('accepts a UUID and short opaque ids unchanged', () => {
    expect(brand(UUID)).toBe(UUID);
    expect(brand('t1')).toBe('t1');
    expect(brand('x'.repeat(128))).toBe('x'.repeat(128));
  });

  it.each(HOSTILE)('refuses %p at construction', (value) => {
    expect(() => brand(value)).toThrow(ContractError);
  });

  it.each(RESERVED_OBJECT_KEYS)(
    'refuses the inherited property name %p at construction',
    (value) => {
      expect(() => brand(value)).toThrow(ContractError);
    },
  );

  it('still accepts an id that merely contains a reserved name', () => {
    expect(brand('__proto__x')).toBe('__proto__x');
  });
});

describe('idPathSegment', () => {
  it('returns a valid id as its one path segment', () => {
    expect(idPathSegment(asTrackId(UUID))).toBe(UUID);
    expect(idPathSegment(asPlaylistId('p_1'))).toBe('p_1');
  });

  it.each(HOSTILE)('refuses %p smuggled past the brand with a cast', (value) => {
    expect(() => idPathSegment(value as TrackId)).toThrow(ContractError);
  });

  it('refuses the no-playlist sentinel', () => {
    expect(() => idPathSegment(NO_PLAYLIST_ID as PlaylistId)).toThrow(ContractError);
  });
});

describe.each([
  ['asTrackId', asTrackId, 'TrackId', 'not a valid id shape'],
  ['asPlaylistId', asPlaylistId, 'PlaylistId', 'not a valid id shape'],
  [
    'idPathSegment',
    (value: string) => idPathSegment(value as TrackId),
    'IdPathSegment',
    'not a safe URL path segment',
  ],
] as const)('%s names where the contract broke', (_name, check, at, detail) => {
  it(`throws a ContractError at ${at}: ${detail}`, () => {
    let thrown: unknown = null;
    try {
      check('a/b');
    } catch (error) {
      thrown = error;
    }
    expect(thrown).toBeInstanceOf(ContractError);
    expect(thrown).toMatchObject({ at, message: `API contract violation at ${at}: ${detail}` });
  });
});

describe.each([
  ['parseTrackId', parseTrackId, 'invalid-track-id'],
  ['parsePlaylistId', parsePlaylistId, 'invalid-playlist-id'],
] as const)('%s', (_name, parse, kind) => {
  it('returns the id it was given when it is safe', () => {
    expect(parse('t1')).toEqual({ ok: true, id: 't1' });
  });

  it(`returns a ${kind} error carrying the refused value`, () => {
    expect(parse('a/b')).toEqual({ ok: false, error: { kind, value: 'a/b' } });
  });
});

describe('asFavoriteKey', () => {
  it('keeps the key it was given', () => {
    expect(asFavoriteKey('deezer:track:7')).toBe('deezer:track:7');
  });
});

describe('sourceIdList', () => {
  it('joins each source as provider:external_id, comma separated, in order', () => {
    expect(
      sourceIdList([
        { provider: 'deezer', external_id: '7' },
        { provider: 'musicbrainz', external_id: 'mb-1' },
      ]),
    ).toBe('deezer:7,musicbrainz:mb-1');
  });

  it('is empty for no sources', () => {
    expect(sourceIdList([])).toBe('');
  });
});
