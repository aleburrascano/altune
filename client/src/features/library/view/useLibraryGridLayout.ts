import { useCallback, useState } from 'react';
import { useWindowDimensions, type LayoutChangeEvent } from 'react-native';

import {
  CONTENT_MAX_WIDTH,
  SCREEN_HORIZONTAL_PADDING,
  spacing,
  useWideWebLayout,
} from '@shared/ui';

import {
  avatarColumns,
  cellSize,
  coverColumns,
  wideCoverColumns,
} from '@features/library/core/gridColumns';

export const GRID_GAP = spacing.md;

type LibraryGridKind = 'cover' | 'avatar';
type LibraryGridLayout = {
  columns: number;
  cellSize: number;
  onLayout: (event: LayoutChangeEvent) => void;
};

function useMeasuredWidth() {
  const [measuredWidth, setMeasuredWidth] = useState<number | null>(null);
  const onLayout = useCallback((event: LayoutChangeEvent) => {
    const measured = event.nativeEvent.layout.width;
    if (measured === 0) return;
    setMeasuredWidth(measured);
  }, []);
  return { measuredWidth, onLayout };
}

function gridSpan(isWideWeb: boolean, windowWidth: number, measuredWidth: number | null) {
  if (!isWideWeb) return { width: windowWidth, horizontalPadding: SCREEN_HORIZONTAL_PADDING };
  if (measuredWidth == null) {
    return {
      width: Math.min(windowWidth, CONTENT_MAX_WIDTH),
      horizontalPadding: SCREEN_HORIZONTAL_PADDING,
    };
  }
  return { width: measuredWidth, horizontalPadding: 0 };
}

function gridColumns(kind: LibraryGridKind, isWideWeb: boolean, width: number): number {
  if (kind !== 'cover') return avatarColumns(width);
  return isWideWeb ? wideCoverColumns(width) : coverColumns(width);
}

export function useLibraryGridLayout(kind: LibraryGridKind): LibraryGridLayout {
  const { width: windowWidth } = useWindowDimensions();
  const { measuredWidth, onLayout } = useMeasuredWidth();
  const isWideWeb = useWideWebLayout();
  const { width, horizontalPadding } = gridSpan(isWideWeb, windowWidth, measuredWidth);
  const columns = gridColumns(kind, isWideWeb, width);

  return {
    columns,
    cellSize: Math.max(0, cellSize({ width, columns, gap: GRID_GAP, horizontalPadding })),
    onLayout,
  };
}
