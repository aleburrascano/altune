import type { ReactElement } from 'react';
import { FlatList, type FlatListProps } from 'react-native';

export type HorizontalRailProps<T> = Omit<FlatListProps<T>, 'horizontal'>;

export function HorizontalRail<T>(props: HorizontalRailProps<T>): ReactElement {
  return <FlatList {...props} horizontal showsHorizontalScrollIndicator={false} />;
}
