import { StyleSheet } from 'react-native';

import { clamp } from '@shared/math';
import { spacing, minInteractiveHeight, opacity } from '@shared/ui/theme/tokens';

export const sharedStyles = StyleSheet.create({
  trackRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    gap: spacing.md,
    minHeight: minInteractiveHeight,
  },
  trackInfo: { flex: 1 },
  retryButton: { marginTop: spacing.sm },
  sectionTitle: { marginBottom: spacing.sm },
  pressed: { opacity: opacity.pressed },
});

export const DETAIL_GUTTER = spacing.lg;

export const DISCOGRAPHY_CARD_SIZE = 128;

export const DISCOGRAPHY_SKELETON_CARD_SIZE = 130;

export const RELATED_CARD_WIDTH = 132;

export const WIDE_LEFT_COLUMN_WIDTH = 360;

const GRID_GAP = spacing.md;

const GRID_MIN_COLUMNS = 4;

const GRID_MAX_COLUMNS = 6;

const GRID_MIN_CELL_WIDTH = 150;

export function gridColumnsFor(containerWidth: number): number {
  const fit = Math.floor((containerWidth + GRID_GAP) / (GRID_MIN_CELL_WIDTH + GRID_GAP));
  return clamp(fit, GRID_MIN_COLUMNS, GRID_MAX_COLUMNS);
}

export function gridCellWidthFor(containerWidth: number, columns: number): number {
  const gaps = GRID_GAP * (columns - 1);
  return Math.max(1, Math.floor((containerWidth - gaps) / columns));
}
