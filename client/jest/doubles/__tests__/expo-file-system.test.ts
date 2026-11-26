import { Directory, File, Paths, __fs } from 'expo-file-system';

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

  it('throws when creating a directory that already exists, as the native module does', () => {
    const dir = new Directory(Paths.document, 'telemetry');
    dir.create();

    expect(() => dir.create()).toThrow('already exists');
    expect(dir.exists).toBe(true);
  });

  it('creates idempotently only when the caller asks for it', () => {
    const dir = new Directory(Paths.document, 'telemetry');
    dir.create();

    expect(() => dir.create({ idempotent: true })).not.toThrow();
    expect(dir.exists).toBe(true);
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

  describe('scripted downloads', () => {
    const URL = 'https://cdn.example/t1.mp3';

    it('writes the downloaded marker by default and logs the call', async () => {
      const dest = new File(Paths.cache, 't1.mp3');

      await File.downloadFileAsync(URL, dest);

      expect(__fs.readFile(dest.uri)).toBe(`downloaded:${URL}`);
      expect(__fs.downloads()).toEqual([{ url: URL, uri: dest.uri }]);
    });

    it('runs a one-shot script for the next call only', async () => {
      const dest = new File(Paths.cache, 't1.mp3');
      __fs.scriptNextDownload(() => Promise.reject(new Error('scripted failure')));

      await expect(File.downloadFileAsync(URL, dest)).rejects.toThrow('scripted failure');
      await File.downloadFileAsync(URL, dest);

      expect(dest.exists).toBe(true);
    });

    it('runs a persistent script until it is cleared, and hands over the default', async () => {
      const dest = new File(Paths.cache, 't1.mp3');
      __fs.scriptDownloads((url: string, target: File, options?: object) =>
        __fs.completeDownload(`wrapped:${url}`, target, options),
      );

      await File.downloadFileAsync(URL, dest);
      expect(__fs.readFile(dest.uri)).toBe(`downloaded:wrapped:${URL}`);

      __fs.scriptDownloads(undefined);
      await File.downloadFileAsync(URL, dest);
      expect(__fs.readFile(dest.uri)).toBe(`downloaded:${URL}`);
    });

    it('forgets scripts and the call log on reset', async () => {
      __fs.scriptDownloads(() => Promise.reject(new Error('left over')));
      __fs.reset();

      await File.downloadFileAsync(URL, new File(Paths.cache, 't1.mp3'));

      expect(__fs.downloads()).toHaveLength(1);
    });
  });

  it('resets between tests', () => {
    expect(Object.keys(__fs.allFiles())).toHaveLength(0);
  });
});
