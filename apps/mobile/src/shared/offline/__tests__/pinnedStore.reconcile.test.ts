import { act } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';

import { pinnedUri, usePinnedStore, type PinnedEntry, type PinnedStatus } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

type FsFailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory';

const { __fs } = FileSystem as unknown as {
  __fs: {
    reset(): void;
    seedDirectory(uri: string): void;
    seedFile(uri: string, contents: string): void;
    readFile(uri: string): string | undefined;
    allFiles(): Record<string, string>;
    failNext(kind: FsFailureKind, error?: Error): void;
  };
};

const AUDIO_DIR = 'file:///document/offline-audio';
const INDEX_URI = 'file:///document/offline/pinned.json';

function audioUri(trackId: string): string {
  return `${AUDIO_DIR}/${trackId}.mp3`;
}

function resetStore(
  overrides: Partial<{
    entries: Record<string, PinnedEntry>;
    queue: string[];
    isWorking: boolean;
  }> = {},
): void {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, ...overrides });
}

type PendingCall = {
  ids: readonly string[];
  resolve: (urls: ResolvedAudioUrl[]) => void;
  reject: (err: unknown) => void;
};

function captureAudioUrlCalls(): PendingCall[] {
  const calls: PendingCall[] = [];
  fetchAudioUrlsMock.mockImplementation((ids: string[]) => {
    let resolve!: PendingCall['resolve'];
    let reject!: PendingCall['reject'];
    const promise = new Promise<ResolvedAudioUrl[]>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    calls.push({ ids, resolve, reject });
    return promise;
  });
  return calls;
}

async function flush(rounds = 20): Promise<void> {
  for (let i = 0; i < rounds; i += 1) {
    await Promise.resolve();
  }
}

beforeEach(() => {
  resetStore();
  fetchAudioUrlsMock.mockReset();
});

const MATRIX: [PinnedStatus, boolean, 'ready' | 'queued' | 'dropped'][] = [
  ['queued', true, 'ready'],
  ['queued', false, 'queued'],
  ['downloading', true, 'ready'],
  ['downloading', false, 'queued'],
  ['ready', true, 'ready'],
  ['ready', false, 'dropped'],
  ['failed', true, 'ready'],
  ['failed', false, 'dropped'],
];

function seedCell(trackId: string, status: PinnedStatus, filePresent: boolean): void {
  if (filePresent) __fs.seedFile(audioUri(trackId), 'audio-bytes');
  resetStore({ entries: { [trackId]: { trackId, status } }, isWorking: true });
}

describe('reconcile — state x disk matrix', () => {
  it.each(MATRIX)(
    'a %s entry with file present=%s resolves to %s (worker held idle so the classification is observed cleanly)',
    (status, filePresent, expected) => {
      const trackId = `cell-${status}-${String(filePresent)}`;
      seedCell(trackId, status, filePresent);

      usePinnedStore.getState().reconcile();

      const entry = usePinnedStore.getState().entries[trackId];
      if (expected === 'dropped') {
        expect(entry).toBeUndefined();
      } else if (expected === 'ready') {
        expect(entry).toEqual({ trackId, status: 'ready', uri: audioUri(trackId) });
      } else {
        expect(entry).toEqual({ trackId, status: 'queued' });
      }
    },
  );
});

describe('reconcile — a mixed index resolves every entry independently', () => {
  it('gets every entry right at once, and the requeue set is exactly the queued/downloading entries whose files are gone', () => {
    __fs.seedFile(audioUri('present-ready'), 'audio-bytes');
    __fs.seedFile(audioUri('present-queued'), 'audio-bytes');
    resetStore({
      entries: {
        'present-ready': { trackId: 'present-ready', status: 'ready', uri: 'stale-uri' },
        'present-queued': { trackId: 'present-queued', status: 'queued' },
        'gone-queued': { trackId: 'gone-queued', status: 'queued' },
        'gone-downloading': { trackId: 'gone-downloading', status: 'downloading' },
        'gone-ready': { trackId: 'gone-ready', status: 'ready', uri: 'stale-uri-2' },
        'gone-failed': { trackId: 'gone-failed', status: 'failed' },
      },
      isWorking: true,
    });

    usePinnedStore.getState().reconcile();

    const { entries, queue } = usePinnedStore.getState();
    expect(entries['present-ready']).toEqual({
      trackId: 'present-ready',
      status: 'ready',
      uri: audioUri('present-ready'),
    });
    expect(entries['present-queued']).toEqual({
      trackId: 'present-queued',
      status: 'ready',
      uri: audioUri('present-queued'),
    });
    expect(entries['gone-queued']).toEqual({ trackId: 'gone-queued', status: 'queued' });
    expect(entries['gone-downloading']).toEqual({ trackId: 'gone-downloading', status: 'queued' });
    expect(entries['gone-ready']).toBeUndefined();
    expect(entries['gone-failed']).toBeUndefined();
    expect(Object.keys(entries).sort()).toEqual(
      ['present-ready', 'present-queued', 'gone-queued', 'gone-downloading'].sort(),
    );
    expect([...queue].sort()).toEqual(['gone-downloading', 'gone-queued'].sort());
  });
});

describe('reconcile — a file on disk with no index entry', () => {
  it('leaves an orphan file alone: it is not adopted into the index', () => {
    __fs.seedFile(audioUri('orphan-file'), 'audio-bytes');
    resetStore({ entries: {}, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries).toEqual({});
  });
});

describe('reconcile — idempotence: apply(apply(e)) equals apply(e)', () => {
  it.each(MATRIX)('a %s entry with file present=%s is unchanged by a second reconcile', (status, filePresent) => {
    const trackId = `idem-${status}-${String(filePresent)}`;
    seedCell(trackId, status, filePresent);

    usePinnedStore.getState().reconcile();
    const once = usePinnedStore.getState().entries;
    const onceQueue = usePinnedStore.getState().queue;

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries).toEqual(once);
    expect(usePinnedStore.getState().queue).toEqual(onceQueue);
  });

  it('is idempotent across a whole mixed index at once, in memory and on disk', () => {
    __fs.seedFile(audioUri('present-ready'), 'audio-bytes');
    resetStore({
      entries: {
        'present-ready': { trackId: 'present-ready', status: 'downloading' },
        'gone-queued': { trackId: 'gone-queued', status: 'queued' },
        'gone-ready': { trackId: 'gone-ready', status: 'ready', uri: 'stale' },
      },
      isWorking: true,
    });

    usePinnedStore.getState().reconcile();
    const once = usePinnedStore.getState().entries;
    const onceRaw = __fs.readFile(INDEX_URI);

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries).toEqual(once);
    expect(__fs.readFile(INDEX_URI)).toBe(onceRaw);
  });
});

describe('reconcile — persistence', () => {
  it('overwrites a stale on-disk index with the freshly rebuilt one, proving reconcile itself writes rather than reading back a stale file', () => {
    __fs.seedFile(
      INDEX_URI,
      JSON.stringify({
        'on-disk': { trackId: 'on-disk', status: 'downloading' },
        vanished: { trackId: 'vanished', status: 'ready', uri: 'old-uri' },
      }),
    );
    __fs.seedFile(audioUri('on-disk'), 'audio-bytes');
    resetStore({
      entries: {
        'on-disk': { trackId: 'on-disk', status: 'queued' },
        vanished: { trackId: 'vanished', status: 'ready', uri: 'old-uri' },
      },
      isWorking: true,
    });

    usePinnedStore.getState().reconcile();

    const raw = __fs.readFile(INDEX_URI);
    expect(raw).toBeDefined();
    expect(JSON.parse(raw as string)).toEqual({
      'on-disk': { trackId: 'on-disk', status: 'ready', uri: audioUri('on-disk') },
    });
  });

  it('a fresh launch reads the stale persisted index, and reconcile then corrects it and overwrites the stale file', () => {
    let freshFsModule: { __fs: typeof __fs } | undefined;
    let freshStoreModule: { usePinnedStore: typeof usePinnedStore } | undefined;

    jest.isolateModules(() => {
      freshFsModule = require('expo-file-system') as { __fs: typeof __fs };
      freshFsModule.__fs.seedFile(
        INDEX_URI,
        JSON.stringify({
          relaunched: { trackId: 'relaunched', status: 'ready', uri: audioUri('relaunched') },
        }),
      );
      freshStoreModule = require('../pinnedStore') as { usePinnedStore: typeof usePinnedStore };
    });

    const freshFs = freshFsModule!.__fs;
    const freshStore = freshStoreModule!.usePinnedStore;

    expect(freshStore.getState().entries['relaunched']).toEqual({
      trackId: 'relaunched',
      status: 'ready',
      uri: audioUri('relaunched'),
    });

    freshStore.setState({ isWorking: true });
    freshStore.getState().reconcile();

    expect(freshStore.getState().entries['relaunched']).toBeUndefined();
    expect(JSON.parse(freshFs.readFile(INDEX_URI) as string)).toEqual({});
  });
});

describe('reconcile — malformed index entries', () => {
  it('builds the trackId from the index key, not from a missing entry.trackId field', () => {
    __fs.seedFile(audioUri('clean-key'), 'audio-bytes');
    const malformed = { status: 'ready' } as unknown as PinnedEntry;
    resetStore({ entries: { 'clean-key': malformed }, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['clean-key']).toEqual({
      trackId: 'clean-key',
      status: 'ready',
      uri: audioUri('clean-key'),
    });
  });

  it('self-heals a trackId field that disagrees with its own index key, since the key is the source of truth', () => {
    __fs.seedFile(audioUri('right-key'), 'audio-bytes');
    resetStore({
      entries: { 'right-key': { trackId: 'someone-elses-id', status: 'ready', uri: 'old' } },
      isWorking: true,
    });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['right-key']).toEqual({
      trackId: 'right-key',
      status: 'ready',
      uri: audioUri('right-key'),
    });
    expect(usePinnedStore.getState().entries['someone-elses-id']).toBeUndefined();
  });

  it('drops an entry carrying a status this version does not recognize when its file is gone, instead of trusting it', () => {
    const malformed = { trackId: 'weird-status', status: 'paused' } as unknown as PinnedEntry;
    resetStore({ entries: { 'weird-status': malformed }, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['weird-status']).toBeUndefined();
  });

  it('still trusts the file over an unrecognized status when the file really is there', () => {
    __fs.seedFile(audioUri('weird-status-2'), 'audio-bytes');
    const malformed = { trackId: 'weird-status-2', status: 'paused' } as unknown as PinnedEntry;
    resetStore({ entries: { 'weird-status-2': malformed }, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['weird-status-2']).toEqual({
      trackId: 'weird-status-2',
      status: 'ready',
      uri: audioUri('weird-status-2'),
    });
  });

  it('does not crash and does not fabricate a ready entry for an empty-string index key', () => {
    resetStore({ entries: { '': { trackId: '', status: 'queued' } }, isWorking: true });

    expect(() => usePinnedStore.getState().reconcile()).not.toThrow();
    expect(usePinnedStore.getState().entries['']?.status).not.toBe('ready');
  });
});

describe('reconcile — hostile disk contents', () => {
  it('resolves real entries correctly amid a foreign file and a subdirectory in the audio folder', () => {
    __fs.seedFile(`${AUDIO_DIR}/.DS_Store`, 'noise');
    __fs.seedFile(`${AUDIO_DIR}/sub/leftover.mp3`, 'noise');
    __fs.seedFile(audioUri('real-track'), 'audio-bytes');
    resetStore({
      entries: {
        'real-track': { trackId: 'real-track', status: 'queued' },
        'missing-track': { trackId: 'missing-track', status: 'queued' },
      },
      isWorking: true,
    });

    expect(() => usePinnedStore.getState().reconcile()).not.toThrow();

    const { entries } = usePinnedStore.getState();
    expect(entries['real-track']).toEqual({
      trackId: 'real-track',
      status: 'ready',
      uri: audioUri('real-track'),
    });
    expect(entries['missing-track']).toEqual({ trackId: 'missing-track', status: 'queued' });
  });

  it('does not match a file belonging to a different track whose id merely shares a prefix', () => {
    __fs.seedFile(audioUri('t10'), 'audio-bytes');
    resetStore({ entries: { t1: { trackId: 't1', status: 'queued' } }, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['t1']).toEqual({ trackId: 't1', status: 'queued' });
  });

  it('treats any file matching <trackId>.* as a complete, ready download, including a truncated partial write', () => {
    __fs.seedFile(`${AUDIO_DIR}/partial-track.mp3.part`, 'only-a-few-bytes');
    resetStore({
      entries: { 'partial-track': { trackId: 'partial-track', status: 'downloading' } },
      isWorking: true,
    });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['partial-track']).toEqual({
      trackId: 'partial-track',
      status: 'ready',
      uri: `${AUDIO_DIR}/partial-track.mp3.part`,
    });
  });
});

describe('reconcile — failure injection', () => {
  it('does not crash the launch when listing the audio directory fails', () => {
    resetStore({
      entries: { 't-ready': { trackId: 't-ready', status: 'ready', uri: 'stale-uri' } },
    });
    __fs.failNext('createDirectory', new Error('EIO: i/o error listing offline-audio'));

    expect(() => usePinnedStore.getState().reconcile()).not.toThrow();
  });

  it('leaves a ready entry untouched when listing the audio directory fails, instead of concluding every file is gone', () => {
    resetStore({
      entries: { 't-ready': { trackId: 't-ready', status: 'ready', uri: 'stale-uri' } },
    });
    __fs.failNext('createDirectory', new Error('EIO: i/o error listing offline-audio'));

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['t-ready']).toEqual({
      trackId: 't-ready',
      status: 'ready',
      uri: 'stale-uri',
    });
  });

  it('does not persist an emptied index when listing the audio directory fails', () => {
    __fs.seedFile(INDEX_URI, JSON.stringify({ 't-ready': { trackId: 't-ready', status: 'ready' } }));
    resetStore({
      entries: { 't-ready': { trackId: 't-ready', status: 'ready', uri: 'stale-uri' } },
    });
    __fs.failNext('createDirectory', new Error('EIO: i/o error listing offline-audio'));

    usePinnedStore.getState().reconcile();

    expect(__fs.readFile(INDEX_URI)).toBe(
      JSON.stringify({ 't-ready': { trackId: 't-ready', status: 'ready' } }),
    );
  });

  it('keeps the in-memory index correct for this session even when persisting the rebuilt index to disk fails', () => {
    __fs.seedFile(audioUri('t-ready'), 'audio-bytes');
    resetStore({ entries: { 't-ready': { trackId: 't-ready', status: 'queued' } } });
    __fs.failNext('write', new Error('ENOSPC: no space left on device'));

    expect(() => usePinnedStore.getState().reconcile()).not.toThrow();

    expect(usePinnedStore.getState().entries['t-ready']).toEqual({
      trackId: 't-ready',
      status: 'ready',
      uri: audioUri('t-ready'),
    });
  });
});

describe('reconcile — kicks the download worker for anything it requeues', () => {
  it('starts a fresh url resolution for a track that was mid-download when the app died', () => {
    const calls = captureAudioUrlCalls();
    resetStore({ entries: { interrupted: { trackId: 'interrupted', status: 'downloading' } } });

    usePinnedStore.getState().reconcile();

    expect(calls).toHaveLength(1);
    expect(calls[0]?.ids).toEqual(['interrupted']);
  });

  it('the kicked retry settles the entry instead of leaving it wedged forever', async () => {
    const calls = captureAudioUrlCalls();
    resetStore({ entries: { interrupted: { trackId: 'interrupted', status: 'downloading' } } });

    usePinnedStore.getState().reconcile();

    await act(async () => {
      calls[0]?.resolve([]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['interrupted']?.status).toBe('failed');
    expect(usePinnedStore.getState().queue).toEqual([]);
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });

  it('never kicks the worker when reconcile requeues nothing', () => {
    __fs.seedFile(audioUri('already-ready'), 'audio-bytes');
    const calls = captureAudioUrlCalls();
    resetStore({
      entries: { 'already-ready': { trackId: 'already-ready', status: 'ready', uri: 'stale' } },
    });

    usePinnedStore.getState().reconcile();

    expect(calls).toHaveLength(0);
  });
});

describe('reconcile — product promises', () => {
  it('does not offer a Track as available offline once its downloaded file has vanished from disk', () => {
    resetStore({
      entries: { 'vanished-track': { trackId: 'vanished-track', status: 'ready', uri: audioUri('vanished-track') } },
      isWorking: true,
    });

    usePinnedStore.getState().reconcile();

    expect(pinnedUri('vanished-track')).toBeUndefined();
  });

  it('retries a Track interrupted mid-download on the next launch instead of forgetting it', () => {
    resetStore({ entries: { interrupted: { trackId: 'interrupted', status: 'downloading' } }, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['interrupted']).toEqual({
      trackId: 'interrupted',
      status: 'queued',
    });
  });

  it('makes a Track available offline purely from what is on disk, even when the index never recorded where the file was', () => {
    __fs.seedFile(audioUri('recovered'), 'audio-bytes');
    resetStore({ entries: { recovered: { trackId: 'recovered', status: 'failed' } }, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(pinnedUri('recovered')).toBe(audioUri('recovered'));
  });
});
