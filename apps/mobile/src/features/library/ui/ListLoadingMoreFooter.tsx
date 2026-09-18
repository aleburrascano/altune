import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

/** What every paged library list puts below its last loaded row while the next page is in flight. */
export function ListLoadingMoreFooter(): ReactElement {
  return (
    <View style={styles.footer}>
      <Text variant="caption" tone="tertiary">
        Loading more…
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  footer: { alignItems: 'center', paddingVertical: spacing.lg },
});
