import { type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Skeleton } from '@shared/ui/primitives/Skeleton';
import { radius, spacing } from '@shared/ui/theme/tokens';

/**
 * Shape-matched loading placeholders for the detail bodies, so their content
 * loads with the same skeleton idiom the rest of the app uses (discover,
 * library) instead of a bare centered spinner.
 */
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

const CARD = 130;

export function AlbumCardsSkeleton({ testID }: { testID?: string }): ReactElement {
  return (
    <View
      testID={testID}
      style={styles.cardRow}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      {[0, 1, 2].map((i) => (
        <View key={i}>
          <Skeleton width={CARD} height={CARD} radius={radius.md} />
          <Skeleton width={CARD} height={12} style={styles.cardTitle} />
          <Skeleton width={CARD * 0.6} height={10} style={styles.cardSub} />
        </View>
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
