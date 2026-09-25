import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

type ListLoadingMoreFooterProps = {
  loading: boolean;
  failed: boolean;
  onRetry: (() => void) | undefined;
};

const RETRY_LABEL = "Couldn't load more. Tap to retry";

/** What every paged library list puts below its last loaded row while the next page is in flight or has failed. */
export function ListLoadingMoreFooter({
  loading,
  failed,
  onRetry,
}: ListLoadingMoreFooterProps): ReactElement | null {
  if (failed && !loading) {
    return (
      <Pressable
        testID="library-load-more-retry"
        style={styles.footer}
        onPress={onRetry}
        accessibilityRole="button"
        accessibilityLabel={RETRY_LABEL}
      >
        <Text variant="caption" tone="tertiary">
          {RETRY_LABEL}
        </Text>
      </Pressable>
    );
  }
  if (!loading) return null;
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
