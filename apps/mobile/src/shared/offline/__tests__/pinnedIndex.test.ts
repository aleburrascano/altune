import {
  INDEX_SCHEMA_VERSION,
  downloadingEntry,
  failedEntry,
  loadIndex,
  queuedEntry,
  readyEntry,
  saveIndex,
  setPinnedIndexFileStore,
  type PinnedEntry,
} from '../pinnedIndex';
import { createMemoryFileStore, type MemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import type { StoredDirectory } from '@shared/files/fileStore';
import { asTrackId } from '@shared/api-client/ids';

// #951: a corrupt, truncated or older-shaped pinned.json must not silently become "nothing pinned".

const INDEX_URI = 'memory://document/offline/pinned.json';
const TEMP_URI = `${INDEX_URI}.tmp`;

const t1: PinnedEntry = { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///audio/t1.mp3' };
const t2: PinnedEntry = { trackId: asTrackId('t2'), status: 'queued' };

let store: MemoryFileStore;
let warn: jest.SpyInstance;

beforeEach(() => {
  store = createMemoryFileStore();
  setPinnedIndexFileStore(store);
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warn.mockRestore();
  setPinnedIndexFileStore();
});

function warnings(): string[] {
  return warn.mock.calls.map((call) => String(call[0]));
}

describe('a corrupt or unreadable index is reported, not silently emptied', () => {
  it('a truncated pinned.json logs a warning naming the file and the parse error, without its contents', () => {
    store.files.set(INDEX_URI, '{"schemaVersion":1,"entries":{"secret-track":{"trackId":"secret-track","sta');

    expect(loadIndex()).toEqual({});

    expect(warnings()).toEqual(['[offline] pinned.json is corrupt (SyntaxError); treating it as empty']);
    expect(warnings().join()).not.toContain('secret-track');
  });

  it('an index that cannot be read logs a warning naming the file and the error', () => {
    store.files.set(INDEX_URI, JSON.stringify({ schemaVersion: 1, entries: { t1 } }));
    store.openDirectory = (): StoredDirectory => {
      throw new Error('EIO');
    };

    expect(loadIndex()).toEqual({});

    expect(warnings()).toEqual(['[offline] could not read pinned.json (Error); treating it as empty']);
  });

  it('valid JSON that is not an index logs a warning rather than passing for an empty library', () => {
    store.files.set(INDEX_URI, '[1,2,3]');

    expect(loadIndex()).toEqual({});

    expect(warnings()).toEqual(['[offline] pinned.json is not a pinned index; treating it as empty']);
  });

  it('a missing index is a legitimate empty library and logs nothing', () => {
    expect(loadIndex()).toEqual({});
    expect(warn).not.toHaveBeenCalled();
  });
});

describe('the index carries a schema version and older versions are migrated', () => {
  it('saveIndex stamps the current schema version around the entries', () => {
    saveIndex({ t1 });

    expect(JSON.parse(store.files.get(INDEX_URI)!)).toEqual({
      schemaVersion: INDEX_SCHEMA_VERSION,
      entries: { t1 },
    });
  });

  it('an unversioned index from before versioning is migrated with every entry preserved', () => {
    store.files.set(INDEX_URI, JSON.stringify({ t1, t2 }));

    expect(loadIndex()).toEqual({ t1, t2 });
    expect(warn).not.toHaveBeenCalled();
  });

  it('an index from a newer build is read best-effort with a warning, not dropped', () => {
    store.files.set(INDEX_URI, JSON.stringify({ schemaVersion: 99, entries: { t1 }, added: true }));

    expect(loadIndex()).toEqual({ t1 });
    expect(warnings()).toEqual([
      '[offline] pinned.json has schema version 99, newer than 1; reading what this version understands',
    ]);
  });
});

describe('index writes go through a temp file and a rename', () => {
  it('saveIndex leaves only the committed file behind', () => {
    saveIndex({ t1 });

    expect([...store.files.keys()]).toEqual([INDEX_URI]);
  });

  it('a kill mid-write leaves a half-written temp file, and the previous index still loads', () => {
    saveIndex({ t1, t2 });
    store.files.set(TEMP_URI, '{"schemaVersion":1,"entries":{"t1":');

    expect(loadIndex()).toEqual({ t1, t2 });
    expect(warn).not.toHaveBeenCalled();
  });

  it('a write whose rename fails keeps the previous index on disk', () => {
    saveIndex({ t1, t2 });
    const openDirectory = store.openDirectory;
    store.openDirectory = (name) => {
      const dir = openDirectory(name);
      return {
        ...dir,
        exists: dir.exists,
        openFile: (fileName) => ({
          ...dir.openFile(fileName),
          moveTo: () => {
            throw new Error('killed');
          },
        }),
      };
    };

    saveIndex({});
    store.openDirectory = openDirectory;

    expect(loadIndex()).toEqual({ t1, t2 });
  });

  it('a kill between removing the old index and the rename still loads the completed write', () => {
    store.files.set(TEMP_URI, JSON.stringify({ schemaVersion: 1, entries: { t2 } }));

    expect(loadIndex()).toEqual({ t2 });
  });
});

const TRACK_ID = asTrackId('t1');
const AUDIO_URI = 'file:///document/offline-audio/t1.mp3';

describe('a pinned entry carries only the fields its status has', () => {
  // Compile-time guards: tsc fails if uri goes back to being optional for every status, which is
  // what let a ready entry name no file and a track with no file name a stale one (#1766).
  it('refuses a ready entry with no uri, and a track with no file that carries one', () => {
    // @ts-expect-error a ready entry names the file it downloaded, so its uri is not optional
    const readyWithoutUri: PinnedEntry = { trackId: TRACK_ID, status: 'ready' };
    // @ts-expect-error a queued entry has not downloaded anything yet, so it names no file
    const queuedWithUri: PinnedEntry = { trackId: TRACK_ID, status: 'queued', uri: AUDIO_URI };
    // @ts-expect-error a failed entry's download produced no file either
    const failedWithUri: PinnedEntry = { trackId: TRACK_ID, status: 'failed', uri: AUDIO_URI };

    expect([readyWithoutUri.status, queuedWithUri.status, failedWithUri.status]).toEqual([
      'ready',
      'queued',
      'failed',
    ]);
  });

  it('records a downloaded version only when the download reported one', () => {
    expect(readyEntry(TRACK_ID, AUDIO_URI)).toStrictEqual({
      trackId: TRACK_ID,
      status: 'ready',
      uri: AUDIO_URI,
    });
    expect(readyEntry(TRACK_ID, AUDIO_URI, 'v3')).toStrictEqual({
      trackId: TRACK_ID,
      status: 'ready',
      uri: AUDIO_URI,
      version: 'v3',
    });
  });

  it('builds a queued, downloading or failed entry from its track id alone', () => {
    expect(queuedEntry(TRACK_ID)).toStrictEqual({ trackId: TRACK_ID, status: 'queued' });
    expect(downloadingEntry(TRACK_ID)).toStrictEqual({ trackId: TRACK_ID, status: 'downloading' });
    expect(failedEntry(TRACK_ID)).toStrictEqual({ trackId: TRACK_ID, status: 'failed' });
  });
});
