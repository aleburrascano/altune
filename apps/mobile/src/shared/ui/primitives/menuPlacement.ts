import { spacing } from '../theme/tokens';

// AIDEV-NOTE: Approximate rendered height of one ContextMenu row (paddingVertical
// md on each side + a line of body text). Only used to choose open-up vs
// open-down when a row-anchored menu would otherwise overflow the screen bottom;
// the exact value isn't load-bearing, so a rough constant is fine.
const ESTIMATED_ITEM_HEIGHT = 48;

export type MenuAnchor = {
  /** Y of the trigger's top edge, in window coordinates. */
  top: number;
  /** Y of the trigger's bottom edge, in window coordinates. */
  bottom: number;
  /** Distance from the window's right edge to the trigger's right edge. */
  right: number;
};

// Either anchored below the trigger (top) or above it (bottom) — never both.
export type MenuPlacement =
  | { right: number; top: number }
  | { right: number; bottom: number };

// Decide where to float a row-anchored dropdown: open downward from the
// trigger's bottom edge by default, or flip to open upward from its top edge
// when there isn't enough room below (a row near the bottom of a scroll list).
export function resolveMenuPlacement(params: {
  anchor: MenuAnchor;
  itemCount: number;
  windowHeight: number;
  insetBottom: number;
  gap?: number;
}): MenuPlacement {
  const { anchor, itemCount, windowHeight, insetBottom, gap = spacing.xs } = params;
  const estimatedHeight = itemCount * ESTIMATED_ITEM_HEIGHT;
  const spaceBelow = windowHeight - anchor.bottom - insetBottom;
  if (estimatedHeight + gap > spaceBelow) {
    return { right: anchor.right, bottom: windowHeight - anchor.top + gap };
  }
  return { right: anchor.right, top: anchor.bottom + gap };
}
