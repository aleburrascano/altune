import { useWindowDimensions } from 'react-native';

import { SCREEN_HORIZONTAL_PADDING } from '@shared/ui';

import { GRID_GAP, avatarColumns, cellSize, coverColumns } from '../gridColumns';

type LibraryGridKind = 'cover' | 'avatar';
type LibraryGridLayout = { columns: number; cellSize: number };

export function useLibraryGridLayout(kind: LibraryGridKind): LibraryGridLayout {
  const { width } = useWindowDimensions();
  const columns = kind === 'cover' ? coverColumns(width) : avatarColumns(width);
  return {
    columns,
    cellSize: cellSize({ width, columns, gap: GRID_GAP, horizontalPadding: SCREEN_HORIZONTAL_PADDING }),
  };
}
