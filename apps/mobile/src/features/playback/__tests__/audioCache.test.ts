import * as FileSystem from 'expo-file-system';

import { asTrackId } from '@shared/api-client/ids';
import type { PlaybackTrack } from '@shared/playback/types';

import {
  buildCacheFileName,
  cacheDir,
  evict,
  evictAllCached,
  evictCached,
  extFromUrl,
  findCached,
} from '../audioCache';

import { libraryTrack, previewTrack } from './fixtures';

const { __fs } = FileSystem as unknown as {
  __fs: {
    seedDirectory(uri: string): void;
    seedFile(uri: string, contents: string): void;
    allFiles(): Record<string, string>;
    failNext(kind: 'list' | 'delete', error?: Error): void;
  };
};

const CACHE_DIR_URI = 'file:///cache/audio-prefetch';

function cachedUri(name: string): string {
  return `${CACHE_DIR_URI}/${name}`;
}

function cachedNames(): string[] {
  return Object.keys(__fs.allFiles())
    .map((uri) => uri.slice(CACHE_DIR_URI.length + 1))
    .sort();
}

function libraryTrackWithId(trackId: string): PlaybackTrack {
  return libraryTrack({
    source: { kind: 'library', trackId: asTrackId(trackId) },
    title: `Track ${trackId}`,
  });
}

describe('cacheDir', () => {
  it('creates the audio-prefetch directory under the cache root when missing', () => {
    const dir = cacheDir();
    expect(dir.uri).toBe(CACHE_DIR_URI);
    expect(dir.exists).toBe(true);
  });
});

describe('extFromUrl', () => {
  it.each<[string, string]>([
    ['https://cdn.example/a/track.flac?sig=v1.2', '.flac'],
    ['https://cdn.example/a.v2/track?sig=v1.2', '.mp3'],
    ['', '.mp3'],
  ])('%s -> %s', (url, expected) => {
    expect(extFromUrl(url)).toBe(expected);
  });
});

describe('buildCacheFileName', () => {
  it.each<[string, string, string]>([
    ['v2', '.flac', 't1.v2.flac'],
    ['', '.mp3', 't1..mp3'],
  ])('a name written for version %p and ext %p is found back', (version, ext, name) => {
    __fs.seedFile(cachedUri(buildCacheFileName('t1', version, ext)), 'audio');

    expect(findCached(asTrackId('t1'), version)?.uri).toBe(cachedUri(name));
  });

  it('writes a name evictCached deletes', () => {
    __fs.seedFile(cachedUri(buildCacheFileName('t1', 'v2', '.flac')), 'audio');

    evictCached(asTrackId('t1'));

    expect(cachedNames()).toEqual([]);
  });
});

describe('findCached', () => {
  it('returns the file matching both track id and version, ignoring other versions', () => {
    __fs.seedFile(cachedUri('t1.v1.mp3'), 'old');
    __fs.seedFile(cachedUri('t1.v2.mp3'), 'new');

    expect(findCached(asTrackId('t1'), 'v2')?.uri).toBe(cachedUri('t1.v2.mp3'));
    expect(findCached(asTrackId('t1'), 'v3')).toBeNull();
  });
});

describe('evictCached', () => {
  it('deletes every version of one track and leaves other tracks', () => {
    __fs.seedFile(cachedUri('t1.v1.mp3'), 'a');
    __fs.seedFile(cachedUri('t1.v2.flac'), 'b');
    __fs.seedFile(cachedUri('t10.v1.mp3'), 'c');

    evictCached(asTrackId('t1'));

    expect(cachedNames()).toEqual(['t10.v1.mp3']);
  });

  it('swallows a filesystem listing failure', () => {
    __fs.seedDirectory(CACHE_DIR_URI);
    __fs.failNext('list');
    expect(() => evictCached(asTrackId('t1'))).not.toThrow();
  });

  it('keeps deleting the remaining versions when one delete fails', () => {
    __fs.seedFile(cachedUri('t1.v1.mp3'), 'a');
    __fs.seedFile(cachedUri('t1.v2.mp3'), 'b');
    __fs.seedFile(cachedUri('t1.v3.mp3'), 'c');
    __fs.failNext('delete', new Error('EBUSY'));

    expect(() => evictCached(asTrackId('t1'))).not.toThrow();

    expect(cachedNames()).toEqual(['t1.v1.mp3']);
  });
});

// The cleanup the sign-out path needs (#1722): retention has no say once the user it was
// prefetched for is gone.
describe('evictAllCached', () => {
  it('deletes every track and version, including the ones the window would have kept', () => {
    __fs.seedFile(cachedUri('t1.v1.mp3'), 'a');
    __fs.seedFile(cachedUri('t1.v2.flac'), 'b');
    __fs.seedFile(cachedUri('t2.v1.mp3'), 'c');

    evictAllCached();

    expect(cachedNames()).toEqual([]);
  });

  it('deletes an entry whose name yields no track id, which the window pass keeps forever', () => {
    __fs.seedFile(cachedUri('.leftover'), 'a');

    evictAllCached();

    expect(cachedNames()).toEqual([]);
  });

  it('keeps deleting the remaining entries when one delete fails', () => {
    __fs.seedFile(cachedUri('t1.v1.mp3'), 'a');
    __fs.seedFile(cachedUri('t2.v1.mp3'), 'b');
    __fs.seedFile(cachedUri('t3.v1.mp3'), 'c');
    __fs.failNext('delete', new Error('EBUSY'));

    expect(() => evictAllCached()).not.toThrow();

    expect(cachedNames()).toEqual(['t1.v1.mp3']);
  });

  it('swallows a filesystem listing failure', () => {
    __fs.seedDirectory(CACHE_DIR_URI);
    __fs.failNext('list');
    expect(() => evictAllCached()).not.toThrow();
  });
});

describe('evict — KEEP_WINDOW retention', () => {
  it('keeps library tracks from the current index through current+4 and deletes the rest', () => {
    const ordered = ['t0', 't1', 't2', 't3', 't4', 't5', 't6'].map(libraryTrackWithId);
    for (const t of ['t0', 't1', 't2', 't3', 't4', 't5', 't6'])
      __fs.seedFile(cachedUri(`${t}.v1.mp3`), t);

    evict(ordered, 1);

    expect(cachedNames()).toEqual([
      't1.v1.mp3',
      't2.v1.mp3',
      't3.v1.mp3',
      't4.v1.mp3',
      't5.v1.mp3',
    ]);
  });

  it('keeps evicting the rest of the pass when one stale entry fails to delete', () => {
    for (const t of ['t0', 't1', 't2', 't3']) __fs.seedFile(cachedUri(`${t}.v1.mp3`), t);
    __fs.failNext('delete', new Error('EBUSY'));

    expect(() => evict([libraryTrackWithId('t3')], 0)).not.toThrow();

    expect(cachedNames()).toEqual(['t0.v1.mp3', 't3.v1.mp3']);
  });

  it('over the byte cap, drops window files farthest from the current track first', () => {
    const ids = ['t0', 't1', 't2', 't3', 't4'];
    for (const t of ids) __fs.seedFile(cachedUri(`${t}.v1.mp3`), 'x'.repeat(10));

    evict(ids.map(libraryTrackWithId), 0, 30);

    expect(cachedNames()).toEqual(['t0.v1.mp3', 't1.v1.mp3', 't2.v1.mp3']);
  });

  it('never drops the current or next track to meet the byte cap', () => {
    const ids = ['t0', 't1', 't2'];
    for (const t of ids) __fs.seedFile(cachedUri(`${t}.v1.mp3`), 'x'.repeat(10));

    evict(ids.map(libraryTrackWithId), 0, 5);

    expect(cachedNames()).toEqual(['t0.v1.mp3', 't1.v1.mp3']);
  });

  it('counts every cached version of a track against the byte cap', () => {
    __fs.seedFile(cachedUri('t0.v1.mp3'), 'x'.repeat(10));
    __fs.seedFile(cachedUri('t1.v1.mp3'), 'x'.repeat(10));
    __fs.seedFile(cachedUri('t2.v1.mp3'), 'x'.repeat(5));
    __fs.seedFile(cachedUri('t2.v2.mp3'), 'x'.repeat(5));

    evict(['t0', 't1', 't2'].map(libraryTrackWithId), 0, 25);

    expect(cachedNames()).toEqual(['t0.v1.mp3', 't1.v1.mp3']);
  });

  it('keeps the whole window while it stays under the default byte cap', () => {
    const ids = ['t0', 't1', 't2', 't3', 't4'];
    for (const t of ids) __fs.seedFile(cachedUri(`${t}.v1.mp3`), t);

    evict(ids.map(libraryTrackWithId), 0);

    expect(cachedNames()).toEqual(ids.map((t) => `${t}.v1.mp3`));
  });

  it('preview tracks in the window keep nothing on disk', () => {
    __fs.seedFile(cachedUri('t0.v1.mp3'), 'x');
    evict([previewTrack({ title: 'A Preview' })], 0);
    expect(cachedNames()).toEqual([]);
  });

  it('swallows a filesystem listing failure', () => {
    __fs.seedDirectory(CACHE_DIR_URI);
    __fs.failNext('list');
    expect(() => evict([], 0)).not.toThrow();
  });
});
