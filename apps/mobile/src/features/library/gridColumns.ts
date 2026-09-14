const TABLET_MIN_WIDTH = 700;
const WIDE_MIN_WIDTH = 1000;

export function coverColumns(width: number): number {
  if (width >= WIDE_MIN_WIDTH) return 4;
  if (width >= TABLET_MIN_WIDTH) return 3;
  return 2;
}

export function avatarColumns(width: number): number {
  if (width >= WIDE_MIN_WIDTH) return 6;
  if (width >= TABLET_MIN_WIDTH) return 5;
  return 3;
}

export function cellSize(input: {
  width: number;
  columns: number;
  horizontalPadding: number;
  gap: number;
}): number {
  const available = input.width - input.horizontalPadding * 2 - input.gap * (input.columns - 1);
  return Math.floor(available / input.columns);
}
