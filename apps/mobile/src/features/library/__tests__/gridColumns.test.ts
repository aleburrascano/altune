import { avatarColumns, cellSize, coverColumns } from '../ui/gridColumns';

describe('coverColumns', () => {
  it('keeps 2 columns on phone widths', () => {
    expect(coverColumns(390)).toBe(2);
    expect(coverColumns(430)).toBe(2);
  });

  it('widens on tablet and wide windows', () => {
    expect(coverColumns(768)).toBe(3);
    expect(coverColumns(1024)).toBe(4);
  });

  it('returns to the phone layout for a narrow split-view window', () => {
    expect(coverColumns(500)).toBe(2);
  });
});

describe('avatarColumns', () => {
  it('fits more avatars than covers at every width', () => {
    for (const width of [390, 768, 1024]) {
      expect(avatarColumns(width)).toBeGreaterThan(coverColumns(width));
    }
  });
});

describe('cellSize', () => {
  it('divides the space left after padding and gaps', () => {
    expect(cellSize({ width: 400, columns: 2, horizontalPadding: 16, gap: 12 })).toBe(178);
  });

  it('floors so a row of cells can never overflow its container', () => {
    const size = cellSize({ width: 401, columns: 3, horizontalPadding: 16, gap: 12 });
    expect(size * 3 + 12 * 2 + 16 * 2).toBeLessThanOrEqual(401);
  });
});
