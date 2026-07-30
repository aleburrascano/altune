import fc from 'fast-check';

import { extFromUrl, formatBytes } from '../pinnedFiles';

describe('law: extFromUrl always returns a non-empty extension beginning with a dot', () => {
  it('holds for any string input', () => {
    fc.assert(
      fc.property(fc.string(), (url) => {
        const ext = extFromUrl(url);

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
