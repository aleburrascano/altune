import * as FileSystem from 'expo-file-system';

import { asTrackId, type TrackId } from '@shared/api-client/ids';

import {
  deleteAllPinned,
  deletePinned,
  downloadPinned,
  extFromUrl,
  findPinned,
  formatBytes,
  pinnedBytes,
  pinnedDir,
  pinnedFilesByTrackId,
  setPinnedFileStore,
  withPinnedBytesCached,
} from '../pinnedFiles';
import type { StoredDirectory, StoredFile } from '@shared/files/fileStore';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

type FsFailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory' | 'list';

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

const PINNED_DIR_URI = 'file:///document/offline-audio';

function pinnedUri(name: string): string {
  return `${PINNED_DIR_URI}/${name}`;
}

describe('extFromUrl', () => {
  it.each<[string, string, string]>([
    [
      'strips the query string before parsing, so a dot in the signature does not leak into the extension',
      'https://cdn.example.com/audio/track123?sig=v1.2',
      '.mp3',
    ],
    [
      'keeps the dot found after the query strip, in the actual filename',
      'https://cdn.example.com/audio/track123.mp3?sig=abc&exp=999',
      '.mp3',
    ],
    [
      'a dot in a directory segment (before the last slash) does not count as an extension',
      'https://cdn.example.com/audio.v2/track123',
      '.mp3',
    ],
    [
      'a dot in the filename (after the last slash) does count, at the exact dot > slash boundary',
      'https://cdn.example.com/audio.v2/track123.flac',
      '.flac',
    ],
    ['a dotless path falls back to the default', 'https://cdn.example.com/audio/track123', '.mp3'],
    ['an empty string falls back to the default', '', '.mp3'],
  ])('%s', (_label, url, expected) => {
    expect(extFromUrl(url)).toBe(expected);
  });
});

describe('extFromUrl — malformed or hostile presigned urls degrade safely', () => {
  it('a bare filename with no path still yields its extension', () => {
    expect(extFromUrl('track123.wav')).toBe('.wav');
  });

  it('a bare filename with neither path nor extension falls back to the default', () => {
    expect(extFromUrl('track123')).toBe('.mp3');
  });

  it('a dot only inside the query signature is ignored, falling back to the default', () => {
    expect(extFromUrl('https://cdn.example.com/audio/track123?sig=a.b.c')).toBe('.mp3');
  });

  it('a bare "?" with nothing before it falls back to the default', () => {
    expect(extFromUrl('?')).toBe('.mp3');
  });

  it('a trailing dot with no characters after it falls back to the default instead of reporting an empty extension', () => {
    expect(extFromUrl('https://cdn.example.com/audio/track123.')).toBe('.mp3');
  });
});

describe('formatBytes', () => {
  it.each<[string, number, string]>([
    ['the < 1024 arm', 1023, '1023 B'],
    ['zero bytes', 0, '0 B'],
    ['exactly 1024, the first KB boundary', 1024, '1.0 KB'],
    ['exactly 1024^2 - 1, one byte short of a full MB', 1024 * 1024 - 1, '1024 KB'],
    ['exactly 1024^2, the first MB boundary', 1024 * 1024, '1.0 MB'],
    ['exactly 1024^3, the first GB boundary', 1024 * 1024 * 1024, '1.0 GB'],
    ['value < 10 still uses toFixed(1)', 9 * 1024, '9.0 KB'],
    ['value === 10 switches to Math.round', 10 * 1024, '10 KB'],
    [
      'a terabyte clamps at GB instead of walking off the units array',
      1024 * 1024 * 1024 * 1024,
      '1024 GB',
    ],
  ])('%s: formatBytes(%i) === %s', (_label, bytes, expected) => {
    expect(formatBytes(bytes)).toBe(expected);
  });
});

describe('pinnedDir', () => {
  it('creates the pinned directory under the document root when absent, on first run', () => {
    const dir = pinnedDir();

    expect(dir.uri).toBe(PINNED_DIR_URI);
    expect(dir.exists).toBe(true);
  });

  it('does not attempt to recreate the directory once it already exists', () => {
    __fs.seedDirectory(PINNED_DIR_URI);
    __fs.failNext('createDirectory', new Error('create() should not have been called'));

    expect(() => pinnedDir()).not.toThrow();
    expect(pinnedDir().uri).toBe(PINNED_DIR_URI);
  });
});

describe('pinnedFilesByTrackId', () => {
  it('reports null when the directory exists but cannot be listed, and a map again once the failure clears', () => {
    __fs.seedDirectory(PINNED_DIR_URI);
    __fs.failNext('list', new Error('permission denied'));

    expect(pinnedFilesByTrackId()).toBeNull();
    expect(pinnedFilesByTrackId()).toEqual(new Map());
  });

  it('keys each file by the track id before its extension, skipping names that are not a track id plus exactly one extension', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio');
    __fs.seedFile(pinnedUri('t10.flac.part'), 'audio');
    __fs.seedFile(pinnedUri('.DS_Store'), 'junk');
    __fs.seedFile(pinnedUri('no-extension'), 'junk');
    __fs.seedDirectory(pinnedUri('t2.leftover'));

    const byTrackId = pinnedFilesByTrackId();

    expect([...(byTrackId?.keys() ?? [])].sort()).toEqual(['t1']);
    expect(byTrackId?.get('t1')?.uri).toBe(pinnedUri('t1.mp3'));
    expect(byTrackId?.has('t10')).toBe(false);
  });

  it('agrees with findPinned on which file a track owns when two files share its id', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio');
    __fs.seedFile(pinnedUri('t1.flac'), 'audio');

    expect(pinnedFilesByTrackId()?.get('t1')?.uri).toBe(findPinned(asTrackId('t1'))?.uri);
  });
});

describe('findPinned', () => {
  it('matches a pinned file by trackId prefix', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio-bytes');

    expect(findPinned(asTrackId('t1'))?.uri).toBe(pinnedUri('t1.mp3'));
  });

  it('does not match a filename that merely contains the trackId elsewhere, only one that starts with it', () => {
    __fs.seedFile(pinnedUri('other-t1.mp3'), 'audio-bytes');

    expect(findPinned(asTrackId('t1'))).toBeNull();
  });

  it('returns null when no file matches the trackId', () => {
    __fs.seedFile(pinnedUri('t2.mp3'), 'audio-bytes');

    expect(findPinned(asTrackId('t1'))).toBeNull();
  });

  it('does not treat a stray subdirectory as a pinned file, even when its name matches the track id', () => {
    __fs.seedDirectory(PINNED_DIR_URI);
    __fs.seedDirectory(pinnedUri('t1.leftover'));

    expect(findPinned(asTrackId('t1'))).toBeNull();
  });

  it('an empty trackId is refused instead of prefix-matching a dotfile (#944)', () => {
    __fs.seedFile(pinnedUri('.DS_Store'), 'junk');

    expect(findPinned('' as TrackId)).toBeNull();
  });

  it('returns null instead of throwing when the pinned directory cannot be created', () => {
    __fs.failNext('createDirectory', new Error('disk full'));

    expect(findPinned(asTrackId('t1'))).toBeNull();
  });
});

describe('deletePinned', () => {
  it('deletes the matching pinned file for a track', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio-bytes');

    expect(deletePinned(asTrackId('t1'))).toBe(true);

    expect(__fs.readFile(pinnedUri('t1.mp3'))).toBeUndefined();
  });

  it('does nothing when the track has no pinned file', () => {
    expect(deletePinned(asTrackId('missing'))).toBe(true);
  });

  it('reports a delete failure instead of throwing, e.g. an OS-locked file currently playing', () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio-bytes');
    __fs.failNext('delete', new Error('file is locked'));

    expect(deletePinned(asTrackId('t1'))).toBe(false);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('t1.mp3'));
    warn.mockRestore();
    expect(__fs.readFile(pinnedUri('t1.mp3'))).toBe('audio-bytes');
  });
});

describe('deleteAllPinned', () => {
  it('deletes every pinned file', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'a');
    __fs.seedFile(pinnedUri('t2.flac'), 'b');

    expect(deleteAllPinned()).toBe(true);

    expect(__fs.allFiles()).toEqual({});
  });

  it('continues past a file that fails to delete instead of aborting the whole pass', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'a');
    __fs.seedFile(pinnedUri('t2.mp3'), 'b');
    __fs.failNext('delete', new Error('t1 is locked'));
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);

    expect(deleteAllPinned()).toBe(false);
    warn.mockRestore();

    expect(__fs.allFiles()).toEqual({ [pinnedUri('t1.mp3')]: 'a' });
  });

  it('is a no-op instead of throwing when the pinned directory cannot be created', () => {
    __fs.failNext('createDirectory', new Error('disk full'));

    expect(() => deleteAllPinned()).not.toThrow();
  });
});

describe('pinnedBytes', () => {
  it('sums the sizes of every pinned file', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), '12345');
    __fs.seedFile(pinnedUri('t2.flac'), '123');

    expect(pinnedBytes()).toBe(8);
  });

  it('returns 0 when nothing is pinned', () => {
    expect(pinnedBytes()).toBe(0);
  });

  it('returns 0 instead of throwing when the pinned directory cannot be created', () => {
    __fs.failNext('createDirectory', new Error('disk full'));

    expect(pinnedBytes()).toBe(0);
  });
});

describe('downloadPinned', () => {
  it('writes the download under the pinned directory, named by trackId and the parsed extension', async () => {
    const uri = await downloadPinned(
      asTrackId('t1'),
      'https://cdn.example.com/audio/t1.flac?sig=abc123',
    );

    expect(uri).toBe(pinnedUri('t1.flac'));
    expect(__fs.readFile(pinnedUri('t1.flac'))).toBe(
      'downloaded:https://cdn.example.com/audio/t1.flac?sig=abc123',
    );
  });

  it('returns the local file uri, never the source url', async () => {
    const url = 'https://cdn.example.com/audio/t1.flac?sig=SECRET-TOKEN&exp=999';

    const uri = await downloadPinned(asTrackId('t1'), url);

    expect(uri).not.toBe(url);
    expect(uri).toBe(pinnedUri('t1.flac'));
  });

  it('never embeds the presigned query string in the on-disk filename', async () => {
    const url = 'https://cdn.example.com/audio/t1.flac?sig=SECRET-TOKEN&exp=999';

    const uri = await downloadPinned(asTrackId('t1'), url);

    expect(uri).not.toContain('SECRET-TOKEN');
    expect(uri).not.toContain('?');
    expect(Object.keys(__fs.allFiles())).toEqual([pinnedUri('t1.flac')]);
  });

  it('propagates a network failure mid-download instead of leaving a silently untracked pin', async () => {
    __fs.failNext('download', new Error('network drop mid-transfer'));

    await expect(
      downloadPinned(asTrackId('t1'), 'https://cdn.example.com/audio/t1.mp3'),
    ).rejects.toThrow('network drop mid-transfer');
    expect(findPinned(asTrackId('t1'))).toBeNull();
  });

  it('propagates a directory-creation failure instead of resolving with no file written', async () => {
    __fs.failNext('createDirectory', new Error('disk full'));

    await expect(
      downloadPinned(asTrackId('t1'), 'https://cdn.example.com/audio/t1.mp3'),
    ).rejects.toThrow('disk full');
  });
});

describe('track id shape guard (#944)', () => {
  const hostile = ['', '.', '..', '../evil', 'a/b', '../../document/x'];

  // The shape guard above runs on ids cast past the brand; this pins the brand that makes such a
  // cast the only way in. tsc fails if these start accepting a bare string again.
  it('refuses a bare string where a TrackId belongs', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio');

    // @ts-expect-error a raw string must go through asTrackId / parseTrackId first
    expect(findPinned('t1')?.uri).toBe(pinnedUri('t1.mp3'));
  });

  it.each(hostile)('findPinned refuses %p', (trackId) => {
    __fs.seedFile(pinnedUri('.hidden'), 'junk');
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio');

    expect(findPinned(trackId as TrackId)).toBeNull();
  });

  it.each(hostile)('deletePinned refuses %p and removes nothing', (trackId) => {
    __fs.seedFile(pinnedUri('.hidden'), 'junk');
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio');
    const before = __fs.allFiles();

    expect(deletePinned(trackId as TrackId)).toBe(true);
    expect(__fs.allFiles()).toEqual(before);
  });

  it.each(hostile)('downloadPinned refuses %p without writing a file', async (trackId) => {
    const before = __fs.allFiles();

    await expect(
      downloadPinned(trackId as TrackId, 'https://cdn.example.com/a.mp3'),
    ).rejects.toThrow('invalid track id');
    expect(__fs.allFiles()).toEqual(before);
  });
});

describe('an injected FileStore scopes the pinned files to it', () => {
  let store: MemoryFileStore;

  beforeEach(() => {
    store = createMemoryFileStore();
    setPinnedFileStore(store);
  });

  afterEach(() => {
    setPinnedFileStore();
  });

  it('downloads, finds, totals and deletes against the injected store, leaving the device mock untouched', async () => {
    const uri = await downloadPinned(asTrackId('t1'), 'https://cdn.example.com/audio/t1.flac');

    expect(uri).toBe('memory://document/offline-audio/t1.flac');
    expect(findPinned(asTrackId('t1'))?.uri).toBe(uri);
    expect(pinnedBytes()).toBe(store.files.get(uri)?.length);
    expect(__fs.allFiles()).toEqual({});

    expect(deleteAllPinned()).toBe(true);
    expect(store.files.size).toBe(0);
  });

  it('restoring the default binding points back at the device filesystem', async () => {
    setPinnedFileStore();

    const uri = await downloadPinned(asTrackId('t1'), 'https://cdn.example.com/audio/t1.mp3');

    expect(uri).toBe(pinnedUri('t1.mp3'));
    expect(store.files.size).toBe(0);
  });
});

// A handle that cannot report its size, as a platform that fails to stat a file it has just
// written would.
function unsized(file: StoredFile): StoredFile {
  return {
    uri: file.uri,
    get exists() {
      return file.exists;
    },
    size: null,
    textSync: () => file.textSync(),
    write: (contents) => file.write(contents),
    delete: () => file.delete(),
    moveTo: (dest) => file.moveTo(dest),
  };
}

describe('the byte total a pass keeps instead of re-listing the directory', () => {
  const T1_MEMORY_URI = 'memory://document/offline-audio/t1.mp3';
  let store: MemoryFileStore;
  let warn: jest.SpyInstance;

  beforeEach(() => {
    store = createMemoryFileStore();
    setPinnedFileStore(store);
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
    setPinnedFileStore();
  });

  it('grows by each completed download, so a later track is measured against what the pass wrote', async () => {
    const total = await withPinnedBytesCached(async () => {
      await downloadPinned(asTrackId('t1'), 'https://cdn.example.com/audio/t1.mp3');
      return pinnedBytes();
    });

    expect(total).toBe(store.files.get(T1_MEMORY_URI)?.length);
  });

  it('is measured again once a completed download reports no size to add', async () => {
    const openDirectory = store.openDirectory;
    store.openDirectory = (name): StoredDirectory => {
      const dir = openDirectory(name);
      return {
        uri: dir.uri,
        get exists() {
          return dir.exists;
        },
        create: () => dir.create(),
        list: () => dir.list(),
        openFile: (fileName) => unsized(dir.openFile(fileName)),
      };
    };

    const total = await withPinnedBytesCached(async () => {
      await downloadPinned(asTrackId('t1'), 'https://cdn.example.com/audio/t1.mp3');
      return pinnedBytes();
    });

    expect(total).toBe(store.files.get(T1_MEMORY_URI)?.length);
  });

  it('is measured again once a failed download leaves behind a partial file it cannot delete', async () => {
    store.download = (_url, dest) => {
      dest.write('partial');
      return Promise.reject(new Error('network drop mid-transfer'));
    };
    store.files.delete = () => {
      throw new Error('EBUSY: file is locked');
    };

    const total = await withPinnedBytesCached(async () => {
      await expect(
        downloadPinned(asTrackId('t1'), 'https://cdn.example.com/audio/t1.mp3'),
      ).rejects.toThrow('network drop mid-transfer');
      return pinnedBytes();
    });

    expect(total).toBe('partial'.length);
  });
});
