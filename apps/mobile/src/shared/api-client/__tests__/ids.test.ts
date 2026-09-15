// #944: a branded TrackId/PlaylistId must actually have the safe id shape, and every id reaches a
// URL path segment through the one always-encoding, shape-checking helper.

import { ContractError } from '../errors';
import {
  asPlaylistId,
  asTrackId,
  idPathSegment,
  NO_PLAYLIST_ID,
  type PlaylistId,
  type TrackId,
} from '../ids';

const UUID = '0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a';
const HOSTILE = ['', '.', '..', '../x', 'a/b', 'a?b=1', 'a#frag', 'a%2Fb', 'a b', 'x'.repeat(129)];

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
