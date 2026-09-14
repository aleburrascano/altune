import { asPlaylistId } from '@shared/api-client/ids';

import { asRepeatMode, fromWireSource, toWireSource } from '../queueStateWire';

describe('toWireSource — store source to snake_case wire shape', () => {
  it('maps null to null', () => {
    expect(toWireSource(null)).toBeNull();
  });

  it('maps a playlist source to playlist_id and name', () => {
    expect(
      toWireSource({ kind: 'playlist', playlistId: asPlaylistId('p1'), name: 'Chill' }),
    ).toEqual({ kind: 'playlist', playlist_id: 'p1', name: 'Chill' });
  });

  it('maps search and library sources', () => {
    expect(toWireSource({ kind: 'search', query: 'jazz' })).toEqual({
      kind: 'search',
      query: 'jazz',
    });
    expect(toWireSource({ kind: 'library' })).toEqual({ kind: 'library' });
  });
});

describe('fromWireSource — wire shape back to a store source', () => {
  it('maps null and undefined to null', () => {
    expect(fromWireSource(null)).toBeNull();
    expect(fromWireSource(undefined)).toBeNull();
  });

  it('round-trips a playlist source', () => {
    expect(fromWireSource({ kind: 'playlist', playlist_id: 'p1', name: 'Chill' })).toEqual({
      kind: 'playlist',
      playlistId: 'p1',
      name: 'Chill',
    });
  });

  it('defaults missing optional fields to empty strings', () => {
    expect(fromWireSource({ kind: 'playlist' })).toEqual({
      kind: 'playlist',
      playlistId: '',
      name: '',
    });
    expect(fromWireSource({ kind: 'search' })).toEqual({ kind: 'search', query: '' });
  });

  it('maps library', () => {
    expect(fromWireSource({ kind: 'library' })).toEqual({ kind: 'library' });
  });
});

describe('asRepeatMode — narrowing an untrusted repeat mode', () => {
  it.each(['off', 'all', 'one'])('accepts %s', (mode) => {
    expect(asRepeatMode(mode)).toBe(mode);
  });

  it.each(['ALL', '', null, undefined, 1])('rejects %p', (value) => {
    expect(asRepeatMode(value)).toBeNull();
  });
});
