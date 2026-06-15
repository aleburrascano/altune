import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

export function SectionHeader({
  title,
  onSeeAll,
  testID,
}: {
  title: string;
  onSeeAll?: () => void;
  testID?: string;
}): ReactElement {
  return (
    <View testID={testID} style={styles.sectionHeader}>
      <Text variant="title">{title}</Text>
      {onSeeAll != null ? (
        <Pressable onPress={onSeeAll} hitSlop={8}>
          <Text variant="label" tone="accent">
            See All ›
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingTop: spacing.xl,
    paddingBottom: spacing.sm,
  },
});
