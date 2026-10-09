import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, minInteractiveHeight, spacing } from '@shared/ui';

type AlreadyAddedRowProps = {
  title: string;
};

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.lg,
  },
  title: { flex: 1 },
});

const TITLE_PROPS = {
  variant: 'body',
  numberOfLines: 1,
  tone: 'tertiary',
  style: styles.title,
} as const;
const ADDED_PROPS = { variant: 'caption', tone: 'tertiary' } as const;

export function AlreadyAddedRow({ title }: AlreadyAddedRowProps): ReactElement {
  return (
    <View style={styles.row}>
      <Text {...TITLE_PROPS}>{title}</Text>
      <Text {...ADDED_PROPS}>Added</Text>
    </View>
  );
}
