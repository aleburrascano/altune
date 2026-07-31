import { capEntries, dedupeById, withEnvelope } from '../outbox';
import type { OutboxEntry } from '../outbox';
import type { DiscoveryEvent } from '../recordEvent';

function entry(eventId: string, overrides: Partial<OutboxEntry> = {}): OutboxEntry {
  return {
    type: 'play',
    event_id: eventId,
    client_occurred_at: '2026-01-01T00:00:00.000Z',
    ...overrides,
  };
}

describe('capEntries at the length<=max boundary', () => {
  it.each<[string, number, number]>([
    ['one below max keeps every entry', 3, 2],
    ['exactly at max keeps every entry', 3, 3],
    ['one above max drops only the oldest entry', 3, 4],
  ])('%s', (_label, max, length) => {
    const xs = Array.from({ length }, (_, i) => entry(`id-${i}`));
    const result = capEntries(xs, max);

    if (length <= max) {
      expect(result).toEqual(xs);
      expect(result).not.toBe(xs);
      return;
    }

    expect(result.map((e) => e.event_id)).toEqual(['id-1', 'id-2', 'id-3']);
  });
});

describe('capEntries with max === 0', () => {
  it('leaves an already-empty queue empty', () => {
    expect(capEntries([], 0)).toEqual([]);
  });

  it('drops every entry regardless of how many there are', () => {
    const xs = [entry('a'), entry('b'), entry('c')];
    expect(capEntries(xs, 0)).toEqual([]);
  });
});

describe('dedupeById', () => {
  it('returns an empty array for an empty queue', () => {
    expect(dedupeById([])).toEqual([]);
  });

  it('keeps every entry when all event_ids are distinct', () => {
    const xs = [entry('a'), entry('b'), entry('c')];
    expect(dedupeById(xs)).toEqual(xs);
  });

  it('on a plain collision, the later entry wins', () => {
    const older = entry('x');
    const newer = entry('x');
    expect(dedupeById([older, newer])).toEqual([newer]);
  });

  it('on a collision where the later entry carries different data, the later data wins at the earlier position', () => {
    const first = entry('x', { payload: { attempt: 1 } });
    const middle = entry('y');
    const last = entry('x', { payload: { attempt: 2 } });

    const result = dedupeById([first, middle, last]);

    expect(result).toEqual([last, middle]);
  });
});

describe('withEnvelope', () => {
  it('stamps a bare event with the given id and timestamp', () => {
    const event: DiscoveryEvent = { type: 'play' };

    const result = withEnvelope(event, 'id-1', '2026-01-01T00:00:00.000Z');

    expect(result).toEqual({
      type: 'play',
      event_id: 'id-1',
      client_occurred_at: '2026-01-01T00:00:00.000Z',
    });
  });

  it('overrides an event_id and client_occurred_at the event already carries', () => {
    const event: DiscoveryEvent = {
      type: 'play',
      event_id: 'stale-id',
      client_occurred_at: '2020-01-01T00:00:00.000Z',
    };

    const result = withEnvelope(event, 'fresh-id', '2026-01-01T00:00:00.000Z');

    expect(result.event_id).toBe('fresh-id');
    expect(result.client_occurred_at).toBe('2026-01-01T00:00:00.000Z');
  });

  it('preserves the rest of the event unchanged', () => {
    const event: DiscoveryEvent = {
      type: 'result_clicked',
      query_norm: 'q',
      payload: { rank: 2 },
    };

    const result = withEnvelope(event, 'id-1', '2026-01-01T00:00:00.000Z');

    expect(result.query_norm).toBe('q');
    expect(result.payload).toEqual({ rank: 2 });
  });

  it('re-enveloping an already-enveloped entry with the same id and timestamp is a no-op', () => {
    const event: DiscoveryEvent = { type: 'play' };
    const once = withEnvelope(event, 'id-1', '2026-01-01T00:00:00.000Z');

    const twice = withEnvelope(once, 'id-1', '2026-01-01T00:00:00.000Z');

    expect(twice).toEqual(once);
  });
});
