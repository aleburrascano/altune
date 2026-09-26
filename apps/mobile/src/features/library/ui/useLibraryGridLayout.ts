import { useCallback, useState } from 'react';
import { useWindowDimensions, type LayoutChangeEvent } from 'react-native';

import { CONTENT_MAX_WIDTH, SCREEN_HORIZONTAL_PADDING, useIsWideWebLayout } from '@shared/ui';

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
  const isWideWeb = useIsWideWebLayout();

  const onLayout = useCallback((event: LayoutChangeEvent) => {
    setMeasuredWidth(event.nativeEvent.layout.width);
  }, []);

  const fallbackWidth = Math.min(windowWidth, CONTENT_MAX_WIDTH);
  const width = measuredWidth ?? fallbackWidth;
  const horizontalPadding = measuredWidth != null ? 0 : SCREEN_HORIZONTAL_PADDING;
  const coverColumnsFor = isWideWeb ? wideCoverColumns : coverColumns;
  const columns = kind === 'cover' ? coverColumnsFor(width) : avatarColumns(width);

  return {
    columns,
    cellSize: cellSize({ width, columns, gap: GRID_GAP, horizontalPadding }),
    onLayout,
  };
}
