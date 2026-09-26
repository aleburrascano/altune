import { ContractError } from '@shared/errors';
import { asPlaylistId } from '@shared/api-client/ids';
import { canPlay } from '@shared/playback/canPlay';

import {
  asAcquisitionStatus,
  asRepeatMode,
  fromWireSource,
  parseQueueState,
  toWireSource,
} from '../queueStateWire';

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

describe('fromWireSource — unrecognized kind', () => {
  it('logs and drops an unrecognized kind instead of reporting the library', () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const wire = { kind: 'album' } as unknown as Parameters<typeof fromWireSource>[0];

    expect(fromWireSource(wire)).toBeNull();
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('unrecognized kind'));
    warn.mockRestore();
  });
});

describe('parseQueueState — the queue-state parse boundary', () => {
  const wire = () => ({
    track_ids: ['t1', 't2'],
    current_index: 1,
    position_ms: 1500,
    shuffled: true,
    repeat_mode: 'all',
    source: { kind: 'search', query: 'jazz' },
    natural_order: ['t2', 't1'],
    current_track: {
      id: 't2',
      title: 'Two',
      artist: 'Artist',
      artwork_url: null,
      duration_seconds: 120,
      acquisition_status: 'ready',
    },
  });

  it('returns the typed state for a well-formed body', () => {
    expect(parseQueueState(wire())).toEqual({ ok: true, state: wire() });
  });

  it('keeps playlist source fields and drops unknown extra fields', () => {
    const result = parseQueueState({
      ...wire(),
      source: { kind: 'playlist', playlist_id: 'p1', name: 'Chill', extra: 1 },
      source_id: 'playlist:p1',
    });
    expect(result.ok && result.state.source).toEqual({
      kind: 'playlist',
      playlist_id: 'p1',
      name: 'Chill',
    });
    expect(result.ok && 'source_id' in result.state).toBe(false);
  });

  it('defaults an absent natural_order, source and current_track for legacy rows', () => {
    const legacy: Record<string, unknown> = wire();
    delete legacy.natural_order;
    delete legacy.source;
    delete legacy.current_track;
    const result = parseQueueState({ ...legacy, natural_order: null });
    expect(result).toEqual({ ok: true, state: { ...legacy, natural_order: [], source: null } });
  });

  it.each<[string, unknown]>([
    ['a kind a newer app version added', { kind: 'album', name: 'Kind of Blue' }],
    ['a non-string kind', { kind: 5 }],
    ['a source carrying no kind', { playlist_id: 'p1' }],
  ])('keeps the saved queue and drops just the source for %s', (_label, source) => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);

    const result = parseQueueState({ ...wire(), source });

    expect(result).toEqual({ ok: true, state: { ...wire(), source: null } });
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('unrecognized kind'));
    warn.mockRestore();
  });

  it.each<[string, unknown, string]>([
    ['a non-object body', null, 'QueueStateResponse'],
    ['non-array track_ids', { ...wire(), track_ids: null }, 'QueueStateResponse.track_ids'],
    ['a non-string id', { ...wire(), track_ids: ['t1', 2] }, 'QueueStateResponse.track_ids[1]'],
    ['a fractional index', { ...wire(), current_index: 1.5 }, 'QueueStateResponse.current_index'],
    ['a negative position', { ...wire(), position_ms: -1 }, 'QueueStateResponse.position_ms'],
    ['a string shuffled', { ...wire(), shuffled: 'yes' }, 'QueueStateResponse.shuffled'],
    ['a non-string repeat_mode', { ...wire(), repeat_mode: 1 }, 'QueueStateResponse.repeat_mode'],
    [
      'a non-string source name',
      { ...wire(), source: { kind: 'playlist', name: 3 } },
      'QueueStateResponse.source.name',
    ],
    [
      'a bad current_track',
      { ...wire(), current_track: { ...wire().current_track, title: null } },
      'QueueStateResponse.current_track.title',
    ],
  ])('rejects %s with a ContractError naming the field', (_label, body, at) => {
    const result = parseQueueState(body);
    expect(result.ok).toBe(false);
    expect(!result.ok && result.error).toBeInstanceOf(ContractError);
    expect(!result.ok && result.error.at).toBe(at);
  });

  it('rethrows a failure that is not a contract violation', () => {
    const body = Object.defineProperty({}, 'track_ids', {
      get() {
        throw new TypeError('boom');
      },
    });
    expect(() => parseQueueState(body)).toThrow(TypeError);
  });
});

describe('asAcquisitionStatus', () => {
  it('keeps the known statuses', () => {
    expect(asAcquisitionStatus('ready', 'x')).toBe('ready');
    expect(asAcquisitionStatus('pending', 'x')).toBe('pending');
    expect(asAcquisitionStatus('failed', 'x')).toBe('failed');
  });

  it('carries an unrecognized status as not playable instead of throwing', () => {
    const status = asAcquisitionStatus('transcoding', 'x');
    expect(status).toBe('failed');
    expect(canPlay(status)).toBe(false);
  });

  it('rejects a non-string status', () => {
    expect(() => asAcquisitionStatus(3, 'x')).toThrow();
  });
});
