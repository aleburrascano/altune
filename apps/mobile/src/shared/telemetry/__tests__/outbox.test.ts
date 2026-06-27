import { capEntries, dedupeById, makeEventId, withEnvelope, type OutboxEntry } from '../outbox';

function entry(id: string): OutboxEntry {
  return withEnvelope({ type: 'library_add' }, id, '2026-06-27T00:00:00.000Z');
}

describe('makeEventId', () => {
  it('produces a v4-shaped uuid', () => {
    expect(makeEventId()).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
  });
  it('is unique across calls', () => {
    expect(makeEventId()).not.toBe(makeEventId());
  });
});

describe('withEnvelope', () => {
  it('stamps event_id and client_occurred_at without losing the event', () => {
    const e = withEnvelope({ type: 'wrong_album', search_id: 's' }, 'id-1', 't');
    expect(e).toEqual({
      type: 'wrong_album',
      search_id: 's',
      event_id: 'id-1',
      client_occurred_at: 't',
    });
  });
});

describe('dedupeById', () => {
  it('keeps the last entry per event_id', () => {
    const first = { ...entry('dup'), search_id: 'a' };
    const second = { ...entry('dup'), search_id: 'b' };
    const out = dedupeById([first, entry('other'), second]);
    expect(out).toHaveLength(2);
    expect(out.find((e) => e.event_id === 'dup')?.search_id).toBe('b');
  });
});

describe('capEntries', () => {
  it('drops the oldest beyond the cap', () => {
    const entries = ['a', 'b', 'c', 'd'].map(entry);
    const out = capEntries(entries, 2);
    expect(out.map((e) => e.event_id)).toEqual(['c', 'd']);
  });
  it('is a no-op under the cap', () => {
    const entries = [entry('a')];
    expect(capEntries(entries, 5)).toEqual(entries);
  });
});
