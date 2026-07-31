import * as FileSystem from 'expo-file-system';

import { loadPersistedOutbox, persistOutbox } from '../outboxStore';
import type { OutboxEntry } from '../outbox';

type FsFailureKind = 'write' | 'read' | 'delete' | 'createDirectory';

const { __fs } = FileSystem as unknown as {
  __fs: {
    reset(): void;
    seedFile(uri: string, contents: string): void;
    readFile(uri: string): string | undefined;
    failNext(kind: FsFailureKind, error?: Error): void;
  };
};

const OUTBOX_FILE_URI = 'file:///document/telemetry/critical-outbox.json';

function entry(overrides: Partial<OutboxEntry> = {}): OutboxEntry {
  return {
    type: 'play',
    event_id: 'e1',
    client_occurred_at: '2026-07-31T00:00:00.000Z',
    ...overrides,
  };
}

function seed(raw: unknown): void {
  __fs.seedFile(OUTBOX_FILE_URI, JSON.stringify(raw));
}

describe('persistOutbox / loadPersistedOutbox — round trip', () => {
  it('writes entries to disk and reads back exactly what was written', () => {
    const entries = [entry({ event_id: 'e1' }), entry({ event_id: 'e2', type: 'skip' })];

    persistOutbox(entries);

    expect(loadPersistedOutbox()).toEqual(entries);
  });

  it('round-trips a payload with nested objects, a null value, and unicode intact', () => {
    const entries = [
      entry({
        event_id: 'e1',
        payload: {
          nested: { deep: { value: 1 } },
          missing: null,
          label: 'a track named ünicode 山',
        },
      }),
    ];

    persistOutbox(entries);

    expect(loadPersistedOutbox()).toEqual(entries);
  });

  it('round-trips a large queue of 500 entries without losing or reordering any', () => {
    const entries = Array.from({ length: 500 }, (_, i) => entry({ event_id: `e${i}` }));

    persistOutbox(entries);

    expect(loadPersistedOutbox()).toEqual(entries);
  });

  it('draining the queue to empty deletes the file rather than leaving a stale one behind', () => {
    persistOutbox([entry({ event_id: 'e1' })]);
    expect(__fs.readFile(OUTBOX_FILE_URI)).toBeDefined();

    persistOutbox([]);

    expect(__fs.readFile(OUTBOX_FILE_URI)).toBeUndefined();
    expect(loadPersistedOutbox()).toEqual([]);
  });
});

describe('loadPersistedOutbox — legacy shapes still in the wild', () => {
  it('no file at all yields an empty queue, e.g. first launch', () => {
    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('an empty file yields an empty queue instead of throwing out of JSON.parse', () => {
    __fs.seedFile(OUTBOX_FILE_URI, '');

    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('a JSON empty array yields an empty queue', () => {
    seed([]);

    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('an entry missing client_occurred_at still loads, since only event_id is validated', () => {
    const legacy = { type: 'play', event_id: 'e1' };
    seed([legacy]);

    expect(loadPersistedOutbox()).toEqual([legacy]);
  });

  it('an entry carrying an unknown key written by a newer app version loads intact rather than being stripped', () => {
    const fromNewerVersion = { ...entry({ event_id: 'e1' }), future_field: 'v2-only' };
    seed([fromNewerVersion]);

    expect(loadPersistedOutbox()).toEqual([fromNewerVersion]);
  });

  it('a null payload round-trips as null rather than being coerced', () => {
    const withNullPayload = { ...entry({ event_id: 'e1' }), payload: null };
    seed([withNullPayload]);

    expect(loadPersistedOutbox()).toEqual([withNullPayload]);
  });

  it('a missing payload field loads intact, since payload is optional', () => {
    const withoutPayload = { type: 'play', event_id: 'e1', client_occurred_at: '2026-07-31T00:00:00.000Z' };
    seed([withoutPayload]);

    expect(loadPersistedOutbox()).toEqual([withoutPayload]);
  });
});

describe('loadPersistedOutbox — adversarial: the on-disk file is a trust boundary', () => {
  it('non-JSON bytes degrade to an empty queue instead of throwing', () => {
    __fs.seedFile(OUTBOX_FILE_URI, 'not json at all {{{');

    expect(() => loadPersistedOutbox()).not.toThrow();
    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('a truncated JSON document degrades to an empty queue', () => {
    __fs.seedFile(OUTBOX_FILE_URI, '[{"event_id":"e1","type":"play"');

    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('a bare JSON string document is rejected rather than treated as one entry', () => {
    seed('e1');

    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('a bare JSON number document is rejected', () => {
    __fs.seedFile(OUTBOX_FILE_URI, '42');

    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('an array holding null, numbers, strings and nested arrays filters down to nothing, without throwing', () => {
    seed([null, 42, 'a-string', [1, 2]]);

    expect(() => loadPersistedOutbox()).not.toThrow();
    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('drops only the corrupt elements, keeping every valid entry alongside them', () => {
    const good = entry({ event_id: 'keep-me' });
    const alsoGood = entry({ event_id: 'keep-me-too', type: 'wrong_album' });
    seed([null, good, 42, alsoGood, [1, 2], 'a-string']);

    expect(loadPersistedOutbox()).toEqual([good, alsoGood]);
  });

  it('an entry with a numeric event_id is filtered out', () => {
    seed([{ ...entry(), event_id: 123 }]);

    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('an entry with an empty-string event_id is rejected, since it cannot be deduped or retried', () => {
    seed([entry({ event_id: '' })]);

    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('two entries with an empty-string event_id are both rejected rather than collapsing into one', () => {
    seed([entry({ event_id: '', type: 'library_add' }), entry({ event_id: '', type: 'wrong_album' })]);

    expect(loadPersistedOutbox()).toEqual([]);
  });

  it('a __proto__ key on an entry loads as a plain own property, without polluting Object.prototype', () => {
    __fs.seedFile(
      OUTBOX_FILE_URI,
      '[{"event_id":"e1","type":"play","__proto__":{"polluted":true}}]',
    );

    const result = loadPersistedOutbox();

    expect((Object.prototype as Record<string, unknown>)['polluted']).toBeUndefined();
    expect(result).toHaveLength(1);
    expect(result[0]?.event_id).toBe('e1');
  });
});

describe('loadPersistedOutbox — every early-return arm and filter conjunct as a row', () => {
  const validEntry = entry({ event_id: 'e1' });
  const withoutEventId = { type: 'play', client_occurred_at: '2026-07-31T00:00:00.000Z' };

  it.each<[string, string | undefined, OutboxEntry[]]>([
    ['arm 1 — no file at all', undefined, []],
    ['arm 2 — valid JSON that is not an array', JSON.stringify({ foo: 'bar' }), []],
    ['arm 3 — invalid JSON, caught by the try/catch', '{not valid', []],
    ['arm 4 — a valid array of one valid entry', JSON.stringify([validEntry]), [validEntry]],
    ['conjunct 1 — a non-object element (a string)', JSON.stringify(['not-an-object']), []],
    ['conjunct 2 — a null element', JSON.stringify([null]), []],
    ['conjunct 3 — an element whose event_id is absent', JSON.stringify([withoutEventId]), []],
  ])('%s', (_label, seeded, expected) => {
    if (seeded !== undefined) __fs.seedFile(OUTBOX_FILE_URI, seeded);

    expect(loadPersistedOutbox()).toEqual(expected);
  });

  it('arm 1 short-circuits before ever touching disk, so a read fault queued for it fires on the following real read instead', () => {
    __fs.failNext('read', new Error('should not fire on the no-op'));

    expect(loadPersistedOutbox()).toEqual([]);
    seed([validEntry]);

    expect(loadPersistedOutbox()).toEqual([]);
  });
});

describe('persistOutbox — every branch as a row', () => {
  it('the empty branch with no existing file is a no-op, not a crash', () => {
    persistOutbox([]);

    expect(__fs.readFile(OUTBOX_FILE_URI)).toBeUndefined();
  });

  it('the non-empty branch overwrites an existing file rather than appending to it', () => {
    persistOutbox([entry({ event_id: 'e1' })]);

    persistOutbox([entry({ event_id: 'e2' })]);

    expect(loadPersistedOutbox()).toEqual([entry({ event_id: 'e2' })]);
  });
});

describe('failure injection — a disk failure degrades to in-memory-only, never throws', () => {
  it('a createDirectory failure makes persistOutbox return normally without writing', () => {
    __fs.failNext('createDirectory', new Error('disk full'));

    expect(() => persistOutbox([entry({ event_id: 'e1' })])).not.toThrow();
    expect(__fs.readFile(OUTBOX_FILE_URI)).toBeUndefined();
  });

  it('a createDirectory failure makes loadPersistedOutbox return an empty queue instead of throwing', () => {
    __fs.failNext('createDirectory', new Error('disk full'));

    let result: OutboxEntry[] = [];
    expect(() => {
      result = loadPersistedOutbox();
    }).not.toThrow();
    expect(result).toEqual([]);
  });

  it('a write failure leaves the prior on-disk queue untouched and does not throw', () => {
    persistOutbox([entry({ event_id: 'e1' })]);
    __fs.failNext('write', new Error('disk full'));

    expect(() =>
      persistOutbox([entry({ event_id: 'e1' }), entry({ event_id: 'e2' })]),
    ).not.toThrow();
    expect(loadPersistedOutbox()).toEqual([entry({ event_id: 'e1' })]);
  });

  it('a read failure makes loadPersistedOutbox fall back to an empty queue instead of throwing', () => {
    persistOutbox([entry({ event_id: 'e1' })]);
    __fs.failNext('read', new Error('permission denied'));

    let result: OutboxEntry[] = [];
    expect(() => {
      result = loadPersistedOutbox();
    }).not.toThrow();
    expect(result).toEqual([]);
  });

  it('a delete failure leaves the stale file on disk but still returns normally', () => {
    persistOutbox([entry({ event_id: 'e1' })]);
    __fs.failNext('delete', new Error('file is locked'));

    expect(() => persistOutbox([])).not.toThrow();
    expect(__fs.readFile(OUTBOX_FILE_URI)).toBeDefined();
  });
});

describe('idempotence — applying the same write or read twice yields the same result', () => {
  it('persisting the same queue twice leaves the same content on disk as persisting it once', () => {
    const entries = [entry({ event_id: 'e1' }), entry({ event_id: 'e2' })];

    persistOutbox(entries);
    const afterFirst = __fs.readFile(OUTBOX_FILE_URI);
    persistOutbox(entries);
    const afterSecond = __fs.readFile(OUTBOX_FILE_URI);

    expect(afterSecond).toBe(afterFirst);
    expect(loadPersistedOutbox()).toEqual(entries);
  });

  it('loading an unchanged file twice returns equal results both times', () => {
    persistOutbox([entry({ event_id: 'e1' })]);

    const first = loadPersistedOutbox();
    const second = loadPersistedOutbox();

    expect(second).toEqual(first);
  });

  it('persisting an empty queue when no file exists yet is a no-op, not an error', () => {
    expect(() => persistOutbox([])).not.toThrow();

    expect(__fs.readFile(OUTBOX_FILE_URI)).toBeUndefined();
  });
});
