import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

export function LibraryEmptyMessage({ label }: { label: string }): ReactElement {
  return (
    <View style={styles.empty}>
      <Text variant="body" tone="secondary">
        {label}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  empty: { flex: 1, alignItems: 'center', paddingTop: spacing['3xl'] },
});
