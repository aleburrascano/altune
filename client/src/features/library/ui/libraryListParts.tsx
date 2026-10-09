import type { ReactElement } from 'react';
import { StyleSheet, View, Pressable } from 'react-native';
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
    <Pressable {...RETRY_ACCESSIBILITY} style={footerStyles.footer} onPress={onRetry}>
      <FooterText>{RETRY_LABEL}</FooterText>
    </Pressable>
  );
}

function LoadingFooter(): ReactElement {
  return (
    <View style={footerStyles.footer}>
      <FooterText>Loading more…</FooterText>
    </View>
  );
}

export function ListLoadingMoreFooter({
  loading,
  failed,
  onRetry,
}: ListLoadingMoreFooterProps): ReactElement | null {
  if (failed && !loading) return <RetryFooter onRetry={onRetry} />;
  return loading ? <LoadingFooter /> : null;
}

const footerStyles = StyleSheet.create({
  footer: { alignItems: 'center', paddingVertical: spacing.lg },
});

export const listContent = StyleSheet.create({
  padded: { paddingBottom: spacing['3xl'] },
  empty: { flexGrow: 1 },
});
