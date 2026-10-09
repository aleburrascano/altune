import { StyleSheet } from 'react-native';

import { spacing } from '@shared/ui/theme/tokens';

export const sharedStyles = StyleSheet.create({
  trackRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    gap: spacing.md,
    minHeight: 48,
  },
  trackInfo: { flex: 1 },
  retryButton: { marginTop: spacing.sm },
  sectionTitle: { marginBottom: spacing.sm },
  pressed: { opacity: 0.6 },
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
  return Math.min(GRID_MAX_COLUMNS, Math.max(GRID_MIN_COLUMNS, fit));
}

export function gridCellWidthFor(containerWidth: number, columns: number): number {
  const gaps = GRID_GAP * (columns - 1);
  return Math.max(1, Math.floor((containerWidth - gaps) / columns));
}

export function withAlpha(hex: string, alpha: number): string {
  const value = hex.replace('#', '');
  const r = parseInt(value.slice(0, 2), 16);
  const g = parseInt(value.slice(2, 4), 16);
  const b = parseInt(value.slice(4, 6), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}
