import fc from 'fast-check';

import { asTrackId } from '@shared/apiClient/ids';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import {
  deleteAllPinned,
  downloadPinned,
  formatBytes,
  setPinnedFileStore,
} from '@shared/offline/state/pinnedFiles';

const PINNED_T1 = 'memory://document/offline-audio/t1';

describe('law: a pinned file always takes a non-empty extension beginning with a dot', () => {
  beforeEach(() => {
    setPinnedFileStore(createMemoryFileStore());
  });

  afterEach(() => {
    setPinnedFileStore();
  });

  it('holds for any url', async () => {
    await fc.assert(
      fc.asyncProperty(fc.string(), async (url) => {
        const uri = await downloadPinned(asTrackId('t1'), url);
        deleteAllPinned();
        const ext = uri.slice(PINNED_T1.length);

        expect(ext.length).toBeGreaterThan(0);
        expect(ext.startsWith('.')).toBe(true);
      }),
    );
  });
});

describe('law: formatBytes always ends in one of the four known units', () => {
  it('holds for any non-negative integer', () => {
    fc.assert(
      fc.property(fc.integer({ min: 0, max: Number.MAX_SAFE_INTEGER }), (bytes) => {
        const formatted = formatBytes(bytes);

        expect(/ (B|KB|MB|GB)$/.test(formatted)).toBe(true);
      }),
    );
  });
});
