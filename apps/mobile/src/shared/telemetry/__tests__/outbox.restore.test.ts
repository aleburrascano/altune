import type { DiscoveryEvent } from '../recordEvent';
import type { OutboxEntry } from '../outbox';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

type AppStateChangeHandler = (state: string) => void;

jest.mock('react-native/Libraries/AppState/AppState', () => {
  const listeners: AppStateChangeHandler[] = [];
  return {
    default: {
      currentState: 'active',
      isAvailable: true,
      addEventListener: jest.fn((_type: string, handler: AppStateChangeHandler) => {
        listeners.push(handler);
        return { remove: jest.fn() };
      }),
    },
    __listeners: listeners,
  };
});

type OutboxModule = typeof import('../outbox');
type SupabaseAuthMock = { auth: { getSession: jest.Mock } };
type FailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory' | 'list';

interface FakeFsDouble {
  reset(): void;
  seedFile(uri: string, contents: string): void;
  readFile(uri: string): string | undefined;
  allFiles(): Record<string, string>;
  failNext(kind: FailureKind, error?: Error): void;
}

const OUTBOX_URI = 'file:///document/telemetry/critical-outbox.json';
const EVENTS_PATH = 'POST /v1/discovery/events';

function currentFs(): FakeFsDouble {
  return (require('expo-file-system') as { __fs: FakeFsDouble }).__fs;
}

function configureSession(): void {
  const mod = require('@shared/auth/supabaseClient') as { supabase: SupabaseAuthMock };
  mod.supabase.auth.getSession.mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
}

function bootApp(): OutboxModule {
  configureSession();
  return require('../outbox') as OutboxModule;
}

function coldDisk(): FakeFsDouble {
  const snapshot = currentFs().allFiles();
  jest.resetModules();
  const fs = currentFs();
  for (const [uri, contents] of Object.entries(snapshot)) fs.seedFile(uri, contents);
  return fs;
}

function restartApp(): OutboxModule {
  coldDisk();
  return bootApp();
}

function libraryAdd(trackId: string): DiscoveryEvent {
  return { type: 'library_add', payload: { track_id: trackId } };
}

function readOutboxFile(): OutboxEntry[] | undefined {
  const raw = currentFs().readFile(OUTBOX_URI);
  return raw === undefined ? undefined : (JSON.parse(raw) as OutboxEntry[]);
}

function requestBody(index: number): OutboxEntry {
  return JSON.parse(String(__http.requests[index].body)) as OutboxEntry;
}

beforeEach(() => {
  jest.resetModules();
});

describe('cold restart reads the queue from disk, not from memory', () => {
  it('restores whatever the disk file holds at boot time, even when it differs from what was queued before the restart', async () => {
    const outbox1 = bootApp();
    __http.replyOnce(EVENTS_PATH, { status: 503 });
    await outbox1.enqueueCritical(libraryAdd('trk-in-memory-before-restart'));
    expect(readOutboxFile()).toHaveLength(1);

    jest.resetModules();
    const fs = currentFs();
    fs.seedFile(
      OUTBOX_URI,
      JSON.stringify([
        {
          type: 'library_add',
          payload: { track_id: 'from-disk-only' },
          event_id: 'proof-event-from-disk',
          client_occurred_at: '2020-01-01T00:00:00.000Z',
        },
      ]),
    );
    const outbox2 = bootApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    const before = __http.requests.length;

    await outbox2.flushOutbox();

    expect(__http.requests.length).toBe(before + 1);
    expect(requestBody(before).event_id).toBe('proof-event-from-disk');
    expect(requestBody(before).payload).toMatchObject({ track_id: 'from-disk-only' });
  });
});

describe('the durability promise: a critical event survives a hard kill and delivers once the network returns', () => {
  it('a library_add event queued while the network is down is delivered after a restart, unchanged, once the network recovers', async () => {
    const outbox1 = bootApp();
    __http.replyOnce(EVENTS_PATH, { status: 503 });

    await expect(outbox1.enqueueCritical(libraryAdd('trk-durable'))).resolves.toBeUndefined();

    const persisted = readOutboxFile();
    expect(persisted).toHaveLength(1);
    const mintedEntry = persisted![0]!;

    const outbox2 = restartApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    const before = __http.requests.length;

    await outbox2.flushOutbox();

    expect(__http.requests.length).toBe(before + 1);
    const delivered = requestBody(before);
    expect(delivered.event_id).toBe(mintedEntry.event_id);
    expect(delivered.client_occurred_at).toBe(mintedEntry.client_occurred_at);
    expect(delivered.payload).toMatchObject({ track_id: 'trk-durable' });
    expect(delivered.payload).toHaveProperty('session_id', expect.any(String));
    expect(readOutboxFile()).toBeUndefined();
  });
});

describe('persistence round-trip', () => {
  it('an enqueued critical event is written to disk with exactly the fields it was minted with', async () => {
    const outbox = bootApp();
    __http.fail(EVENTS_PATH);

    await outbox.enqueueCritical(libraryAdd('trk-round-trip'));

    const persisted = readOutboxFile();
    expect(persisted).toEqual([
      {
        type: 'library_add',
        payload: { track_id: 'trk-round-trip' },
        event_id: expect.any(String),
        client_occurred_at: expect.any(String),
      },
    ]);
  });

  it('a successful full drain deletes the outbox file rather than leaving an empty array behind', async () => {
    const outbox = bootApp();
    __http.reply(EVENTS_PATH, { status: 202 });

    await outbox.enqueueCritical(libraryAdd('trk-drained'));

    expect(currentFs().allFiles()[OUTBOX_URI]).toBeUndefined();
    expect(readOutboxFile()).toBeUndefined();
  });
});

describe('failure injection', () => {
  it('a disk write failure with the network up still delivers the event, just without ever making it durable', async () => {
    const outbox = bootApp();
    currentFs().failNext('write', new Error('disk full'));
    __http.reply(EVENTS_PATH, { status: 202 });

    await expect(outbox.enqueueCritical(libraryAdd('trk-write-fails'))).resolves.toBeUndefined();

    expect(__http.countFor(EVENTS_PATH)).toBe(1);
    expect(requestBody(0).payload).toMatchObject({ track_id: 'trk-write-fails' });
    expect(currentFs().allFiles()[OUTBOX_URI]).toBeUndefined();
  });

  it('a network failure with the disk fine leaves the event durable and undelivered, ready for a later attempt', async () => {
    const outbox = bootApp();
    __http.fail(EVENTS_PATH);

    await expect(outbox.enqueueCritical(libraryAdd('trk-network-fails'))).resolves.toBeUndefined();

    expect(__http.countFor(EVENTS_PATH)).toBe(1);
    const persisted = readOutboxFile();
    expect(persisted).toHaveLength(1);
    expect(persisted![0]!.payload).toMatchObject({ track_id: 'trk-network-fails' });
  });

  it('a disk write failure and a network failure together still resolve without throwing, at the cost of losing the event', async () => {
    const outbox = bootApp();
    currentFs().failNext('write', new Error('disk full'));
    __http.fail(EVENTS_PATH);

    await expect(outbox.enqueueCritical(libraryAdd('trk-both-fail'))).resolves.toBeUndefined();

    expect(__http.countFor(EVENTS_PATH)).toBe(1);
    expect(currentFs().allFiles()[OUTBOX_URI]).toBeUndefined();
  });

  it('a read failure while restoring from disk starts the queue empty instead of throwing', async () => {
    const outbox1 = bootApp();
    __http.replyOnce(EVENTS_PATH, { status: 503 });
    await outbox1.enqueueCritical(libraryAdd('trk-unreadable-on-restore'));
    expect(readOutboxFile()).toHaveLength(1);

    const fs = coldDisk();
    fs.failNext('read', new Error('disk read error'));
    const outbox2 = bootApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    const before = __http.requests.length;

    await expect(outbox2.flushOutbox()).resolves.toBeUndefined();

    expect(__http.requests.length).toBe(before);
  });

  it('a delete failure after a successful send leaves the entry on disk, so a later restart redelivers the same event_id', async () => {
    const outbox1 = bootApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    currentFs().failNext('delete', new Error('disk busy'));

    await expect(outbox1.enqueueCritical(libraryAdd('trk-stale-after-delete'))).resolves.toBeUndefined();

    expect(__http.countFor(EVENTS_PATH)).toBe(1);
    const staleEntry = readOutboxFile();
    expect(staleEntry).toHaveLength(1);
    const firstDeliveryId = requestBody(0).event_id;

    const outbox2 = restartApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    const before = __http.requests.length;

    await outbox2.flushOutbox();

    expect(__http.requests.length).toBe(before + 1);
    expect(requestBody(before).event_id).toBe(firstDeliveryId);
    expect(readOutboxFile()).toBeUndefined();
  });
});

describe('within-session duplicate suppression', () => {
  it('a second flushOutbox call in the same session does not resend an entry left on disk by a failed delete', async () => {
    const outbox = bootApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    currentFs().failNext('delete', new Error('disk busy'));

    await outbox.enqueueCritical(libraryAdd('trk-not-resent-same-session'));

    expect(__http.countFor(EVENTS_PATH)).toBe(1);
    expect(readOutboxFile()).toHaveLength(1);

    await outbox.flushOutbox();

    expect(__http.countFor(EVENTS_PATH)).toBe(1);
  });
});

describe('idempotence / replay across restarts', () => {
  it('a restart after a successful send has nothing left to redeliver', async () => {
    const outbox1 = bootApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    await outbox1.enqueueCritical(libraryAdd('trk-already-sent'));
    expect(readOutboxFile()).toBeUndefined();

    const outbox2 = restartApp();
    const before = __http.requests.length;

    await outbox2.flushOutbox();

    expect(__http.requests.length).toBe(before);
  });

  it('a restart mid partial-drain delivers exactly the remainder, not what already succeeded', async () => {
    const outbox1 = bootApp();
    __http.replyOnce(EVENTS_PATH, { status: 503 });
    await outbox1.enqueueCritical(libraryAdd('trk-first'));
    __http.replyOnce(EVENTS_PATH, { status: 503 });
    await outbox1.enqueueCritical(libraryAdd('trk-second'));

    const queuedBeforeRestart = readOutboxFile();
    expect(queuedBeforeRestart).toHaveLength(2);
    const firstId = queuedBeforeRestart![0]!.event_id;
    const secondId = queuedBeforeRestart![1]!.event_id;

    const outbox2 = restartApp();
    __http.replyOnce(EVENTS_PATH, { status: 202 });
    __http.replyOnce(EVENTS_PATH, { status: 503 });
    const beforeDrain = __http.requests.length;

    await outbox2.flushOutbox();

    expect(__http.requests.length).toBe(beforeDrain + 2);
    expect(requestBody(beforeDrain).event_id).toBe(firstId);
    expect(requestBody(beforeDrain + 1).event_id).toBe(secondId);
    const remaining = readOutboxFile();
    expect(remaining).toHaveLength(1);
    expect(remaining![0]!.event_id).toBe(secondId);

    const outbox3 = restartApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    const beforeFinal = __http.requests.length;

    await outbox3.flushOutbox();

    expect(__http.requests.length).toBe(beforeFinal + 1);
    expect(requestBody(beforeFinal).event_id).toBe(secondId);
    expect(readOutboxFile()).toBeUndefined();
  });

  it('two restarts in a row with no intervening enqueue and no successful flush deliver the event exactly once when it finally succeeds', async () => {
    const outbox1 = bootApp();
    __http.replyOnce(EVENTS_PATH, { status: 503 });
    await outbox1.enqueueCritical(libraryAdd('trk-two-restarts'));
    const mintedId = readOutboxFile()![0]!.event_id;

    restartApp();

    const outbox3 = restartApp();
    __http.reply(EVENTS_PATH, { status: 202 });
    const before = __http.requests.length;

    await outbox3.flushOutbox();

    expect(__http.requests.length).toBe(before + 1);
    expect(requestBody(before).event_id).toBe(mintedId);
    expect(readOutboxFile()).toBeUndefined();
  });
});
