import * as FileSystem from 'expo-file-system';

import {
  deleteAllPinned,
  deletePinned,
  downloadPinned,
  extFromUrl,
  findPinned,
  formatBytes,
  pinnedBytes,
  pinnedDir,
  pinnedDirReadable,
} from '../pinnedFiles';

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

describe('pinnedDirReadable', () => {
  it('reports false when the directory exists but cannot be listed, and true again once the failure clears', () => {
    __fs.seedDirectory(PINNED_DIR_URI);
    __fs.failNext('list', new Error('permission denied'));

    expect(pinnedDirReadable()).toBe(false);
    expect(pinnedDirReadable()).toBe(true);
  });
});

describe('findPinned', () => {
  it('matches a pinned file by trackId prefix', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio-bytes');

    expect(findPinned('t1')?.uri).toBe(pinnedUri('t1.mp3'));
  });

  it('does not match a filename that merely contains the trackId elsewhere, only one that starts with it', () => {
    __fs.seedFile(pinnedUri('other-t1.mp3'), 'audio-bytes');

    expect(findPinned('t1')).toBeNull();
  });

  it('returns null when no file matches the trackId', () => {
    __fs.seedFile(pinnedUri('t2.mp3'), 'audio-bytes');

    expect(findPinned('t1')).toBeNull();
  });

  it('does not treat a stray subdirectory as a pinned file, even when its name matches the track id', () => {
    __fs.seedDirectory(PINNED_DIR_URI);
    __fs.seedDirectory(pinnedUri('t1.leftover'));

    expect(findPinned('t1')).toBeNull();
  });

  it('an empty trackId matches any entry whose name begins with a literal dot', () => {
    __fs.seedFile(pinnedUri('.DS_Store'), 'junk');

    expect(findPinned('')?.uri).toBe(pinnedUri('.DS_Store'));
  });

  it('returns null instead of throwing when the pinned directory cannot be created', () => {
    __fs.failNext('createDirectory', new Error('disk full'));

    expect(findPinned('t1')).toBeNull();
  });
});

describe('deletePinned', () => {
  it('deletes the matching pinned file for a track', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio-bytes');

    deletePinned('t1');

    expect(__fs.readFile(pinnedUri('t1.mp3'))).toBeUndefined();
  });

  it('does nothing when the track has no pinned file', () => {
    expect(() => deletePinned('missing')).not.toThrow();
  });

  it('swallows a delete failure instead of throwing, e.g. an OS-locked file currently playing', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'audio-bytes');
    __fs.failNext('delete', new Error('file is locked'));

    expect(() => deletePinned('t1')).not.toThrow();
    expect(__fs.readFile(pinnedUri('t1.mp3'))).toBe('audio-bytes');
  });
});

describe('deleteAllPinned', () => {
  it('deletes every pinned file', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'a');
    __fs.seedFile(pinnedUri('t2.flac'), 'b');

    deleteAllPinned();

    expect(__fs.allFiles()).toEqual({});
  });

  it('continues past a file that fails to delete instead of aborting the whole pass', () => {
    __fs.seedFile(pinnedUri('t1.mp3'), 'a');
    __fs.seedFile(pinnedUri('t2.mp3'), 'b');
    __fs.failNext('delete', new Error('t1 is locked'));

    deleteAllPinned();

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
    const uri = await downloadPinned('t1', 'https://cdn.example.com/audio/t1.flac?sig=abc123');

    expect(uri).toBe(pinnedUri('t1.flac'));
    expect(__fs.readFile(pinnedUri('t1.flac'))).toBe(
      'downloaded:https://cdn.example.com/audio/t1.flac?sig=abc123',
    );
  });

  it('returns the local file uri, never the source url', async () => {
    const url = 'https://cdn.example.com/audio/t1.flac?sig=SECRET-TOKEN&exp=999';

    const uri = await downloadPinned('t1', url);

    expect(uri).not.toBe(url);
    expect(uri).toBe(pinnedUri('t1.flac'));
  });

  it('never embeds the presigned query string in the on-disk filename', async () => {
    const url = 'https://cdn.example.com/audio/t1.flac?sig=SECRET-TOKEN&exp=999';

    const uri = await downloadPinned('t1', url);

    expect(uri).not.toContain('SECRET-TOKEN');
    expect(uri).not.toContain('?');
    expect(Object.keys(__fs.allFiles())).toEqual([pinnedUri('t1.flac')]);
  });

  it('propagates a network failure mid-download instead of leaving a silently untracked pin', async () => {
    __fs.failNext('download', new Error('network drop mid-transfer'));

    await expect(downloadPinned('t1', 'https://cdn.example.com/audio/t1.mp3')).rejects.toThrow(
      'network drop mid-transfer',
    );
    expect(findPinned('t1')).toBeNull();
  });

  it('propagates a directory-creation failure instead of resolving with no file written', async () => {
    __fs.failNext('createDirectory', new Error('disk full'));

    await expect(downloadPinned('t1', 'https://cdn.example.com/audio/t1.mp3')).rejects.toThrow(
      'disk full',
    );
  });
});
