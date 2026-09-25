import { type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Skeleton } from '@shared/ui/primitives/Skeleton';
import { radius, spacing } from '@shared/ui/theme/tokens';

import { DISCOGRAPHY_SKELETON_CARD_SIZE } from './layout';

export function TrackRowsSkeleton({
  count = 6,
  testID,
}: {
  count?: number;
  testID?: string;
}): ReactElement {
  return (
    <View
      testID={testID}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      {Array.from({ length: count }, (_, i) => (
        <View key={i} style={styles.row}>
          <Skeleton width={40} height={40} radius={radius.sm} />
          <View style={styles.rowText}>
            <Skeleton width="65%" height={13} />
            <Skeleton width="40%" height={11} style={styles.rowSub} />
          </View>
          <Skeleton width={24} height={24} radius={12} />
        </View>
      ))}
    </View>
  );
}

function AlbumCardSkeleton(): ReactElement {
  return (
    <View>
      <Skeleton
        width={DISCOGRAPHY_SKELETON_CARD_SIZE}
        height={DISCOGRAPHY_SKELETON_CARD_SIZE}
        radius={radius.md}
      />
      <Skeleton width={DISCOGRAPHY_SKELETON_CARD_SIZE} height={12} style={styles.cardTitle} />
      <Skeleton width={DISCOGRAPHY_SKELETON_CARD_SIZE * 0.6} height={10} style={styles.cardSub} />
    </View>
  );
}

const HIDDEN = {
  accessibilityElementsHidden: true,
  importantForAccessibility: 'no-hide-descendants',
} as const;

export function AlbumCardsSkeleton(): ReactElement {
  return (
    <View testID="detail-discography-skeleton" style={styles.cardRow} {...HIDDEN}>
      {[0, 1, 2].map((i) => (
        <AlbumCardSkeleton key={i} />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.sm,
  },
  rowText: { flex: 1 },
  rowSub: { marginTop: spacing.xs },
  cardRow: { flexDirection: 'row', gap: spacing.md },
  cardTitle: { marginTop: spacing.sm },
  cardSub: { marginTop: spacing.xs },
});
