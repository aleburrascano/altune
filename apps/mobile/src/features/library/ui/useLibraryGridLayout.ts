import { useCallback, useState } from 'react';
import { useWindowDimensions, type LayoutChangeEvent } from 'react-native';

import { CONTENT_MAX_WIDTH, SCREEN_HORIZONTAL_PADDING, useWideWebLayout } from '@shared/ui';

import { GRID_GAP, avatarColumns, cellSize, coverColumns, wideCoverColumns } from '../gridColumns';

type LibraryGridKind = 'cover' | 'avatar';
type LibraryGridLayout = {
  columns: number;
  cellSize: number;
  onLayout: (event: LayoutChangeEvent) => void;
};

export function useLibraryGridLayout(kind: LibraryGridKind): LibraryGridLayout {
  const { width: windowWidth } = useWindowDimensions();
  const [measuredWidth, setMeasuredWidth] = useState<number | null>(null);
  const isWideWeb = useWideWebLayout();

  const onLayout = useCallback((event: LayoutChangeEvent) => {
    const measured = event.nativeEvent.layout.width;
    if (measured === 0) return;
    setMeasuredWidth(measured);
  }, []);

  const fallbackWidth = Math.min(windowWidth, CONTENT_MAX_WIDTH);
  const width = isWideWeb ? measuredWidth ?? fallbackWidth : fallbackWidth;
  const horizontalPadding = isWideWeb && measuredWidth != null ? 0 : SCREEN_HORIZONTAL_PADDING;
  const coverColumnsFor = isWideWeb ? wideCoverColumns : coverColumns;
  const columns = kind === 'cover' ? coverColumnsFor(width) : avatarColumns(width);

  return {
    columns,
    cellSize: Math.max(0, cellSize({ width, columns, gap: GRID_GAP, horizontalPadding })),
    onLayout,
  };
}
