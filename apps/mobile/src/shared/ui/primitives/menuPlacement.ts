import { spacing } from '../theme/tokens';

const ESTIMATED_ITEM_HEIGHT = 48;

export type MenuAnchor = {
  top: number;
  bottom: number;
  right: number;
};

export type MenuPlacement = { right: number; top: number } | { right: number; bottom: number };

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
