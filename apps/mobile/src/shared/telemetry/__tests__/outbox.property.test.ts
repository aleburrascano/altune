import fc from 'fast-check';

import { capEntries, dedupeById, makeEventId, withEnvelope } from '../outbox';
import type { OutboxEntry } from '../outbox';
import type { DiscoveryEventType } from '../recordEvent';

const EVENT_TYPES: DiscoveryEventType[] = [
  'results_shown',
  'result_clicked',
  'play',
  'skip',
  'completed',
  'library_add',
  'wrong_album',
];

const eventTypeArb = fc.constantFrom(...EVENT_TYPES);

const entryArb: fc.Arbitrary<OutboxEntry> = fc.record(
  {
    type: eventTypeArb,
    event_id: fc.uuid(),
    client_occurred_at: fc.integer({ min: 0, max: 4102444800000 }).map((ms) =>
      new Date(ms).toISOString(),
    ),
    query_norm: fc.string({ maxLength: 20 }),
    payload: fc.dictionary(fc.string({ maxLength: 10 }), fc.string({ maxLength: 10 })),
  },
  { requiredKeys: ['type', 'event_id', 'client_occurred_at'] },
);

const idPool = ['a', 'b', 'c', 'd'] as const;
const collidableEntryArb: fc.Arbitrary<OutboxEntry> = fc.record({
  type: eventTypeArb,
  event_id: fc.constantFrom(...idPool),
  client_occurred_at: fc.string({ minLength: 1, maxLength: 12 }),
});

describe('law: capEntries(xs, max).length === Math.min(xs.length, max)', () => {
  it('holds for every array and every non-negative cap', () => {
    fc.assert(
      fc.property(
        fc.array(entryArb, { maxLength: 40 }),
        fc.integer({ min: 0, max: 60 }),
        (xs, max) => {
          expect(capEntries(xs, max).length).toBe(Math.min(xs.length, max));
        },
      ),
      { numRuns: 200 },
    );
  });
});

describe('law: capEntries always returns a trailing slice of the input — the oldest entries are what drop', () => {
  it('the result equals xs sliced to its own length from the end', () => {
    fc.assert(
      fc.property(
        fc.array(entryArb, { maxLength: 40 }),
        fc.integer({ min: 0, max: 60 }),
        (xs, max) => {
          const result = capEntries(xs, max);
          expect(result).toEqual(xs.slice(xs.length - result.length));
        },
      ),
      { numRuns: 200 },
    );
  });
});

describe('law: capEntries never mutates its input and never returns the same array instance', () => {
  it('holds whether or not the cap is exceeded', () => {
    fc.assert(
      fc.property(
        fc.array(entryArb, { maxLength: 40 }),
        fc.integer({ min: 0, max: 60 }),
        (xs, max) => {
          const before = [...xs];
          const result = capEntries(xs, max);
          expect(xs).toEqual(before);
          expect(result).not.toBe(xs);
        },
      ),
      { numRuns: 200 },
    );
  });
});

describe('law: capEntries is idempotent once applied', () => {
  it('capping an already-capped array with the same max changes nothing', () => {
    fc.assert(
      fc.property(
        fc.array(entryArb, { maxLength: 40 }),
        fc.integer({ min: 0, max: 60 }),
        (xs, max) => {
          const once = capEntries(xs, max);
          expect(capEntries(once, max)).toEqual(once);
        },
      ),
      { numRuns: 200 },
    );
  });
});

describe('law: dedupeById keeps exactly one entry per distinct event_id — the last one seen', () => {
  it('length equals the count of distinct ids, and each survivor equals the last entry carrying that id', () => {
    fc.assert(
      fc.property(fc.array(collidableEntryArb, { maxLength: 40 }), (xs) => {
        const result = dedupeById(xs);
        const distinctIds = new Set(xs.map((e) => e.event_id));
        expect(result.length).toBe(distinctIds.size);

        for (const survivor of result) {
          const lastWithId = [...xs].reverse().find((e) => e.event_id === survivor.event_id);
          expect(survivor).toEqual(lastWithId);
        }
      }),
      { numRuns: 200 },
    );
  });
});

describe('law: dedupeById preserves first-occurrence order', () => {
  it('surviving ids appear in the order their id first appeared in the input', () => {
    fc.assert(
      fc.property(fc.array(collidableEntryArb, { maxLength: 40 }), (xs) => {
        const seen = new Set<string>();
        const expectedOrder: string[] = [];
        for (const e of xs) {
          if (!seen.has(e.event_id)) {
            seen.add(e.event_id);
            expectedOrder.push(e.event_id);
          }
        }
        expect(dedupeById(xs).map((e) => e.event_id)).toEqual(expectedOrder);
      }),
      { numRuns: 200 },
    );
  });
});

describe('law: dedupeById is idempotent', () => {
  it('deduping an already-deduped array changes nothing', () => {
    fc.assert(
      fc.property(fc.array(collidableEntryArb, { maxLength: 40 }), (xs) => {
        const once = dedupeById(xs);
        expect(dedupeById(once)).toEqual(once);
      }),
      { numRuns: 200 },
    );
  });
});

describe('law: withEnvelope always stamps the given id and timestamp, whatever the event already carried', () => {
  it('the result carries exactly the given event_id and client_occurred_at', () => {
    fc.assert(
      fc.property(entryArb, fc.string({ maxLength: 30 }), fc.string({ maxLength: 30 }), (event, id, at) => {
        const result = withEnvelope(event, id, at);
        expect(result.event_id).toBe(id);
        expect(result.client_occurred_at).toBe(at);
      }),
      { numRuns: 200 },
    );
  });
});

describe('law: withEnvelope is idempotent for a fixed id and timestamp', () => {
  it('re-enveloping an already-enveloped entry with the same id and timestamp changes nothing', () => {
    fc.assert(
      fc.property(entryArb, fc.string({ maxLength: 30 }), fc.string({ maxLength: 30 }), (event, id, at) => {
        const once = withEnvelope(event, id, at);
        const twice = withEnvelope(once, id, at);
        expect(twice).toEqual(once);
      }),
      { numRuns: 200 },
    );
  });
});

describe('law: makeEventId always produces an RFC4122 v4 shaped id, and does not repeat across a large sample', () => {
  it('matches the v4 pattern and never collides within a batch', () => {
    fc.assert(
      fc.property(fc.integer({ min: 50, max: 300 }), (n) => {
        const ids = Array.from({ length: n }, () => makeEventId());

        for (const id of ids) {
          expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
        }
        expect(new Set(ids).size).toBe(n);
      }),
      { numRuns: 20 },
    );
  });
});
