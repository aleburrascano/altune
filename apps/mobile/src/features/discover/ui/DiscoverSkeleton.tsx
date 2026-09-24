import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Skeleton, radius, spacing } from '@shared/ui';

import { ART_SIZE } from './DiscoverRow';

const SKELETON_ROWS = [0, 1, 2, 3, 4, 5];

function SkeletonRow(): ReactElement {
  return (
    <View style={styles.row}>
      <Skeleton width={ART_SIZE} height={ART_SIZE} radius={radius.md} />
      <View style={styles.text}>
        <Skeleton width="70%" height={14} />
        <Skeleton width="40%" height={12} />
      </View>
    </View>
  );
}

export function DiscoverSkeleton(): ReactElement {
  return (
    <View testID="discover-loading" style={styles.list}>
      {SKELETON_ROWS.map((i) => (
        <SkeletonRow key={i} />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  list: { flex: 1, paddingTop: spacing.sm },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.xs,
  },
  text: { flex: 1, gap: spacing.sm },
});
