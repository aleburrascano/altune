import { avatarColumns, cellSize, coverColumns } from '../ui/gridColumns';

describe('coverColumns — breakpoints at 700 and 1000, inclusive lower bounds', () => {
  it('is 2 below the tablet breakpoint', () => {
    expect(coverColumns(0)).toBe(2);
    expect(coverColumns(699)).toBe(2);
  });

  it('turns to 3 exactly at the tablet breakpoint of 700', () => {
    expect(coverColumns(700)).toBe(3);
    expect(coverColumns(999)).toBe(3);
  });

  it('turns to 4 exactly at the wide breakpoint of 1000', () => {
    expect(coverColumns(1000)).toBe(4);
    expect(coverColumns(2000)).toBe(4);
  });
});

describe('avatarColumns — same breakpoints, denser 3/5/6 ladder', () => {
  it('is 3 below the tablet breakpoint', () => {
    expect(avatarColumns(699)).toBe(3);
  });

  it('turns to 5 exactly at 700 and holds until the wide breakpoint', () => {
    expect(avatarColumns(700)).toBe(5);
    expect(avatarColumns(999)).toBe(5);
  });

  it('turns to 6 exactly at 1000', () => {
    expect(avatarColumns(1000)).toBe(6);
  });
});

describe('cellSize — floor of the width left after padding and inter-cell gaps', () => {
  it('divides the remaining width evenly when it divides cleanly', () => {
    expect(cellSize({ width: 300, columns: 2, horizontalPadding: 10, gap: 20 })).toBe(130);
  });

  it('floors a non-integer cell rather than rounding or returning a fraction', () => {
    expect(cellSize({ width: 301, columns: 2, horizontalPadding: 10, gap: 20 })).toBe(130);
  });

  it('subtracts the gap only between cells, so one column subtracts no gap', () => {
    expect(cellSize({ width: 200, columns: 1, horizontalPadding: 10, gap: 20 })).toBe(180);
  });

  it('subtracts one fewer gap than columns for a three-column grid', () => {
    expect(cellSize({ width: 320, columns: 3, horizontalPadding: 10, gap: 20 })).toBe(86);
  });
});
