import {
  OUTBOX_SCHEMA_VERSION,
  loadPersistedOutbox,
  persistOutbox,
  setOutboxFileStore,
} from '../outboxStore';
import type { OutboxEntry } from '../outbox';
import { createMemoryFileStore, type MemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import type { StoredDirectory } from '@shared/files/fileStore';

// #951: a corrupt, truncated or older-shaped outbox must not silently lose unsent critical events.

const OUTBOX_URI = 'memory://document/telemetry/critical-outbox.json';
const TEMP_URI = `${OUTBOX_URI}.tmp`;

function entry(eventId: string): OutboxEntry {
  return { type: 'library_add', event_id: eventId, client_occurred_at: '2026-09-15T00:00:00.000Z' };
}

let store: MemoryFileStore;
let warn: jest.SpyInstance;

beforeEach(() => {
  store = createMemoryFileStore();
  setOutboxFileStore(store);
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warn.mockRestore();
  setOutboxFileStore();
});

function warnings(): string[] {
  return warn.mock.calls.map((call) => String(call[0]));
}

describe('a corrupt or unreadable outbox is reported, not silently emptied', () => {
  it('a truncated outbox logs a warning naming the file and the parse error, without its contents', () => {
    store.files.set(OUTBOX_URI, '{"schemaVersion":1,"entries":[{"event_id":"secret-event","payl');

    expect(loadPersistedOutbox()).toEqual([]);

    expect(warnings()).toEqual([
      '[telemetry] critical-outbox.json is corrupt (SyntaxError); treating it as empty',
    ]);
    expect(warnings().join()).not.toContain('secret-event');
  });

  it('an outbox that cannot be read logs a warning naming the file and the error', () => {
    store.openDirectory = (): StoredDirectory => {
      throw new Error('EIO');
    };

    expect(loadPersistedOutbox()).toEqual([]);

    expect(warnings()).toEqual([
      '[telemetry] could not read critical-outbox.json (Error); treating it as empty',
    ]);
  });

  it.each<[string, string]>([
    ['a JSON object with no entries', '{"foo":"bar"}'],
    ['a JSON null', 'null'],
  ])('%s logs a warning rather than passing for an empty queue', (_label, contents) => {
    store.files.set(OUTBOX_URI, contents);

    expect(loadPersistedOutbox()).toEqual([]);

    expect(warnings()).toEqual(['[telemetry] critical-outbox.json is not an outbox; treating it as empty']);
  });

  it('a missing outbox is a legitimate empty queue and logs nothing', () => {
    expect(loadPersistedOutbox()).toEqual([]);
    expect(warn).not.toHaveBeenCalled();
  });
});

describe('the outbox carries a schema version and older versions are migrated', () => {
  it('persistOutbox stamps the current schema version around the entries', () => {
    persistOutbox([entry('e1')]);

    expect(JSON.parse(store.files.get(OUTBOX_URI)!)).toEqual({
      schemaVersion: OUTBOX_SCHEMA_VERSION,
      entries: [entry('e1')],
    });
  });

  it('an unversioned outbox array from before versioning is migrated with every entry preserved', () => {
    store.files.set(OUTBOX_URI, JSON.stringify([entry('e1'), entry('e2')]));

    expect(loadPersistedOutbox()).toEqual([entry('e1'), entry('e2')]);
    expect(warn).not.toHaveBeenCalled();
  });

  it('an outbox from a newer build is read best-effort with a warning, not dropped', () => {
    store.files.set(OUTBOX_URI, JSON.stringify({ schemaVersion: 7, entries: [entry('e1')] }));

    expect(loadPersistedOutbox()).toEqual([entry('e1')]);
    expect(warnings()).toEqual([
      '[telemetry] critical-outbox.json has schema version 7, newer than 1; reading what this version understands',
    ]);
  });
});

describe('outbox writes go through a temp file and a rename', () => {
  it('persistOutbox leaves only the committed file behind', () => {
    persistOutbox([entry('e1')]);

    expect([...store.files.keys()]).toEqual([OUTBOX_URI]);
  });

  it('a kill mid-write leaves a half-written temp file, and the previous queue still loads', () => {
    persistOutbox([entry('e1'), entry('e2')]);
    store.files.set(TEMP_URI, '{"schemaVersion":1,"entries":[{"event_');

    expect(loadPersistedOutbox()).toEqual([entry('e1'), entry('e2')]);
    expect(warn).not.toHaveBeenCalled();
  });

  it('a kill between removing the old outbox and the rename still loads the completed write', () => {
    store.files.set(TEMP_URI, JSON.stringify({ schemaVersion: 1, entries: [entry('e3')] }));

    expect(loadPersistedOutbox()).toEqual([entry('e3')]);
  });

  it('draining the queue removes a leftover temp file too, so it cannot resurrect sent events', () => {
    persistOutbox([entry('e1')]);
    store.files.set(TEMP_URI, JSON.stringify({ schemaVersion: 1, entries: [entry('e1')] }));

    persistOutbox([]);

    expect(store.files.size).toBe(0);
    expect(loadPersistedOutbox()).toEqual([]);
  });
});
