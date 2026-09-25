import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

type ListLoadingMoreFooterProps = {
  loading: boolean;
  failed: boolean;
  onRetry: (() => void) | undefined;
};

const RETRY_LABEL = "Couldn't load more. Tap to retry";

function FooterText({ children }: { children: string }): ReactElement {
  return (
    <Text variant="caption" tone="tertiary">
      {children}
    </Text>
  );
}

const RETRY_ACCESSIBILITY = {
  testID: 'library-load-more-retry',
  accessibilityRole: 'button',
  accessibilityLabel: RETRY_LABEL,
} as const;

function RetryFooter({ onRetry }: { onRetry: (() => void) | undefined }): ReactElement {
  return (
    <Pressable {...RETRY_ACCESSIBILITY} style={styles.footer} onPress={onRetry}>
      <FooterText>{RETRY_LABEL}</FooterText>
    </Pressable>
  );
}

function LoadingFooter(): ReactElement {
  return (
    <View style={styles.footer}>
      <FooterText>Loading more…</FooterText>
    </View>
  );
}

/** What every paged library list puts below its last loaded row while the next page is in flight or has failed. */
export function ListLoadingMoreFooter({
  loading,
  failed,
  onRetry,
}: ListLoadingMoreFooterProps): ReactElement | null {
  if (failed && !loading) return <RetryFooter onRetry={onRetry} />;
  return loading ? <LoadingFooter /> : null;
}

const styles = StyleSheet.create({
  footer: { alignItems: 'center', paddingVertical: spacing.lg },
});
