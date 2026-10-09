import { MalformedSSEEventError, isServerEvent, parseChunk } from '@shared/events/sseFrames';
import type { ParsedBlock } from '@shared/events/sseFrames';

describe('parseChunk', () => {
  it('returns nothing for empty input', () => {
    expect(parseChunk('', '', '')).toEqual({ blocks: [], remainder: '', lastEventId: '' });
  });

  it('parses one complete event and advances lastEventId', () => {
    const parsed = parseChunk('', 'id: 7\nevent: resync\ndata: {"a":1}\n\n', '');
    expect(parsed.blocks).toEqual([{ id: '7', type: 'resync', data: { a: 1 } }]);
    expect(parsed.remainder).toBe('');
    expect(parsed.lastEventId).toBe('7');
  });

  it('defaults the type to message', () => {
    const parsed = parseChunk('', 'data: {"a":1}\n\n', '');
    expect(parsed.blocks).toEqual([{ id: '', type: 'message', data: { a: 1 } }]);
  });

  it('yields no block for a comment line', () => {
    const parsed = parseChunk('', ': keepalive\n\n', '');
    expect(parsed.blocks).toEqual([]);
  });

  it('yields no block when id and event have no data', () => {
    const parsed = parseChunk('', 'id: 3\nevent: resync\n\n', '2');
    expect(parsed.blocks).toEqual([]);
    expect(parsed.lastEventId).toBe('2');
  });

  it('reports malformed JSON without advancing lastEventId', () => {
    const parsed = parseChunk('', 'id: 9\nevent: resync\ndata: {not json}\n\n', '4');
    expect(parsed.blocks).toHaveLength(1);
    const block = parsed.blocks[0] as ParsedBlock;
    expect(block).toBeInstanceOf(MalformedSSEEventError);
    expect(isServerEvent(block)).toBe(false);
    const error = block as MalformedSSEEventError;
    expect(error.eventId).toBe('9');
    expect(error.eventType).toBe('resync');
    expect(error.payloadLength).toBe('{not json}'.length);
    expect(parsed.lastEventId).toBe('4');
  });

  it('completes an event split across two calls', () => {
    const first = parseChunk('', 'data: {"a"', '');
    expect(first.blocks).toEqual([]);
    expect(first.remainder).toBe('data: {"a"');
    const second = parseChunk(first.remainder, ':1}\n\n', first.lastEventId);
    expect(second.blocks).toEqual([{ id: '', type: 'message', data: { a: 1 } }]);
    expect(second.remainder).toBe('');
  });

  it.each([
    ['CRLF', '\r\n\r\n'],
    ['bare CR', '\r\r'],
    ['LF', '\n\n'],
  ])('splits blocks on %s separators', (_name, sep) => {
    const parsed = parseChunk('', `data: {"a":1}${sep}data: {"a":2}${sep}`, '');
    expect(parsed.blocks).toEqual([
      { id: '', type: 'message', data: { a: 1 } },
      { id: '', type: 'message', data: { a: 2 } },
    ]);
  });

  it('joins multiple data lines with a newline before parsing', () => {
    const parsed = parseChunk('', 'data: {"a":\ndata: 1}\n\n', '');
    expect(parsed.blocks).toEqual([{ id: '', type: 'message', data: { a: 1 } }]);
  });

  it('keeps the earlier lastEventId when a later event has an empty id', () => {
    const parsed = parseChunk('', 'data: {"a":1}\n\n', '5');
    expect(parsed.lastEventId).toBe('5');
  });
});
