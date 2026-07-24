/**
 * Column counts for the Library grids, derived from the viewport width.
 *
 * `app.json` declares `supportsTablet: true`, but every grid was a hardcoded
 * 2 or 3 columns — on an iPad that renders phone-sized cells stretched across a
 * 1024pt canvas. Breakpoints are on width alone (not a device class) so a
 * split-view or rotated window gets the layout its actual width deserves.
 */

/** Phone portrait ends around here; iPad portrait is 768. */
const TABLET_MIN_WIDTH = 700;
const WIDE_MIN_WIDTH = 1000;

/** Covers/playlists: 2 on phones, 3 on tablets, 4 on a wide window. */
export function coverColumns(width: number): number {
  if (width >= WIDE_MIN_WIDTH) return 4;
  if (width >= TABLET_MIN_WIDTH) return 3;
  return 2;
}

/** Artist avatars are smaller, so they take one more column than covers. */
export function avatarColumns(width: number): number {
  if (width >= WIDE_MIN_WIDTH) return 6;
  if (width >= TABLET_MIN_WIDTH) return 5;
  return 3;
}

/** Cell width for a `columns`-wide grid inside `horizontalPadding`, with
 *  `gap` between cells. Floored so rounding never overflows the row. */
export function cellSize(input: {
  width: number;
  columns: number;
  horizontalPadding: number;
  gap: number;
}): number {
  const available =
    input.width - input.horizontalPadding * 2 - input.gap * (input.columns - 1);
  return Math.floor(available / input.columns);
}
