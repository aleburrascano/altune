import { Directory, File, Paths, __fs } from 'expo-file-system';
import * as SecureStore from 'expo-secure-store';
import TrackPlayer, { __player } from 'react-native-track-player';

const { __secureStore } = SecureStore as unknown as {
  __secureStore: { read(key: string): string | undefined; failNext(op: string, e?: Error): void };
};

describe('filesystem double', () => {
  it('round-trips a write through a read', () => {
    const dir = new Directory(Paths.document, 'telemetry');
    dir.create();
    const file = new File(dir, 'outbox.json');

    file.write('["queued"]');

    expect(file.exists).toBe(true);
    expect(file.textSync()).toBe('["queued"]');
  });

  it('reports a file as absent until it is written', () => {
    const file = new File(Paths.document, 'nothing-here.json');
    expect(file.exists).toBe(false);
    expect(() => file.textSync()).toThrow(/ENOENT/);
  });

  it('forgets contents after delete, so a lost write is observable', () => {
    const file = new File(Paths.document, 'pref.json');
    file.write('"dark"');
    file.delete();
    expect(file.exists).toBe(false);
  });

  it('lists only direct children, as File instances', () => {
    const dir = new Directory(Paths.document, 'offline-audio');
    dir.create();
    new File(dir, 't1.mp3').write('audio');
    new File(dir, 't10.mp3').write('audio');

    const entries = dir.list();

    expect(entries).toHaveLength(2);
    expect(entries.every((entry) => entry instanceof File)).toBe(true);
    expect(entries.map((entry) => entry.uri.split('/').pop()).sort()).toEqual([
      't1.mp3',
      't10.mp3',
    ]);
  });

  it('injects a write failure exactly once', () => {
    const file = new File(Paths.document, 'outbox.json');
    __fs.failNext('write', new Error('disk full'));

    expect(() => file.write('["dropped"]')).toThrow('disk full');
    expect(file.exists).toBe(false);

    file.write('["kept"]');
    expect(file.textSync()).toBe('["kept"]');
  });

  it('injects a listing failure on a directory that exists, distinct from an absent one', () => {
    const dir = new Directory(Paths.document, 'offline-audio');
    dir.create();
    new File(dir, 't1.mp3').write('audio');
    __fs.failNext('list', new Error('EIO: i/o error'));

    expect(() => dir.list()).toThrow('EIO: i/o error');
    expect(dir.exists).toBe(true);
    expect(dir.list()).toHaveLength(1);
  });

  it('resets between tests', () => {
    expect(Object.keys(__fs.allFiles())).toHaveLength(0);
  });
});

describe('secure-store double', () => {
  it('round-trips a token', async () => {
    await SecureStore.setItemAsync('session', 'token-abc');
    await expect(SecureStore.getItemAsync('session')).resolves.toBe('token-abc');
  });

  it('returns null for an absent key rather than throwing', async () => {
    await expect(SecureStore.getItemAsync('missing')).resolves.toBeNull();
  });

  it('injects a keychain failure', async () => {
    __secureStore.failNext('set', new Error('keychain unavailable'));
    await expect(SecureStore.setItemAsync('session', 'token')).rejects.toThrow(
      'keychain unavailable',
    );
    expect(__secureStore.read('session')).toBeUndefined();
  });
});

describe('track-player double', () => {
  it('exposes a stable mock per method, so call assertions hold', async () => {
    expect(TrackPlayer.play).toBe(TrackPlayer.play);

    await TrackPlayer.play();

    expect(TrackPlayer.play).toHaveBeenCalledTimes(1);
  });

  it('injects a rejection on a named method', async () => {
    __player.failNext('skipToNext', new Error('player not initialised'));
    await expect(TrackPlayer.skipToNext()).rejects.toThrow('player not initialised');
  });

  it('drives playback state and progress', () => {
    __player.setState('playing');
    __player.setProgress({ position: 42, duration: 180 });

    const { usePlaybackState, useProgress } = require('react-native-track-player');

    expect(usePlaybackState().state).toBe('playing');
    expect(useProgress()).toEqual({ position: 42, duration: 180, buffered: 0 });
  });

  it('resets call counts between tests', () => {
    expect(TrackPlayer.play).toHaveBeenCalledTimes(0);
  });
});
