import { createFileStoreSlot, deviceFileStore } from '../fileStore';
import { createMemoryFileStore } from './memoryFileStore';

// The seam every persisted store binds through: killSwitch, pinnedFiles, pinnedIndex and
// outboxStore each hold one slot, and their test suites swap and restore it around every case.
describe('a file store slot', () => {
  it('starts bound to the device filesystem when created with no default', () => {
    const slot = createFileStoreSlot();

    expect(slot.get()).toBe(deviceFileStore);
  });

  it('ensureDir() creates a directory that is not there yet', () => {
    const store = createMemoryFileStore();
    const slot = createFileStoreSlot(store);

    const dir = slot.ensureDir('outbox');

    expect(dir.exists).toBe(true);
    expect(store.openDirectory('outbox').exists).toBe(true);
  });

  it('ensureDir() keeps what a directory that already exists holds', () => {
    const slot = createFileStoreSlot(createMemoryFileStore());
    slot.ensureDir('outbox').openFile('a.json').write('kept');

    const dir = slot.ensureDir('outbox');

    expect(dir.openFile('a.json').textSync()).toBe('kept');
  });

  it('writes through the store it was last set to, not the one it replaced', () => {
    const replaced = createMemoryFileStore();
    const slot = createFileStoreSlot(replaced);
    const store = createMemoryFileStore();

    slot.set(store);
    slot.ensureDir('outbox').openFile('a.json').write('written');

    expect(store.openDirectory('outbox').openFile('a.json').textSync()).toBe('written');
    expect(replaced.openDirectory('outbox').exists).toBe(false);
  });

  it('set() with no argument binds the default back', () => {
    const fallback = createMemoryFileStore();
    const slot = createFileStoreSlot(fallback);
    slot.set(createMemoryFileStore());

    slot.set();

    expect(slot.get()).toBe(fallback);
  });
});
