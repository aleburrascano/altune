import { deviceFileStore, type FileStore } from '../fileStore';
import { createMemoryFileStore } from './memoryFileStore';

// The contract any FileStore must satisfy. The device store runs here against the suite-wide
// expo-file-system double; the in-memory store is the scoped fake tests inject into consumers.
describe.each<[string, () => FileStore]>([
  ['deviceFileStore', () => deviceFileStore],
  ['createMemoryFileStore', createMemoryFileStore],
])('%s satisfies the FileStore contract', (_name, makeStore) => {
  let store: FileStore;

  beforeEach(() => {
    store = makeStore();
  });

  it('opens a directory without creating it, and create() makes it exist', () => {
    const dir = store.openDirectory('contract');

    expect(dir.exists).toBe(false);
    dir.create();
    expect(dir.exists).toBe(true);
    expect(store.openDirectory('contract').exists).toBe(true);
  });

  it('list() throws for a directory that does not exist', () => {
    expect(() => store.openDirectory('missing').list()).toThrow();
  });

  it('opens a file without creating it; write, read, size and delete round-trip', () => {
    const dir = store.openDirectory('contract');
    dir.create();
    const file = dir.openFile('a.json');

    expect(file.uri).toBe(`${dir.uri}/a.json`);
    expect(file.exists).toBe(false);
    expect(file.size).toBeNull();
    expect(() => file.textSync()).toThrow();

    file.write('hello');
    expect(file.exists).toBe(true);
    expect(file.size).toBe(5);
    expect(dir.openFile('a.json').textSync()).toBe('hello');

    file.write('replaced');
    expect(file.textSync()).toBe('replaced');

    file.delete();
    expect(file.exists).toBe(false);
  });

  it('list() returns only the files directly inside the directory', () => {
    const dir = store.openDirectory('contract');
    dir.create();
    dir.openFile('a.mp3').write('a');
    dir.openFile('b.mp3').write('b');
    const other = store.openDirectory('contract-other');
    other.create();
    other.openFile('c.mp3').write('c');
    const nested = store.openDirectory('contract/nested');
    nested.create();
    nested.openFile('d.mp3').write('d');

    expect(
      dir
        .list()
        .map((f) => f.uri)
        .sort(),
    ).toEqual([`${dir.uri}/a.mp3`, `${dir.uri}/b.mp3`]);
  });

  it('download() writes into the destination file and resolves to its uri', async () => {
    const dir = store.openDirectory('contract');
    dir.create();
    const dest = dir.openFile('t1.mp3');

    await expect(store.download('https://cdn.example.com/t1.mp3', dest)).resolves.toBe(dest.uri);
    expect(dest.exists).toBe(true);
  });
});
