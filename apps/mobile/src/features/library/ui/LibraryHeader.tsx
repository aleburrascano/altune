import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

export function LibraryHeader(): ReactElement {
  return (
    <View style={styles.header}>
      <Text variant="displayL">Library</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
});
