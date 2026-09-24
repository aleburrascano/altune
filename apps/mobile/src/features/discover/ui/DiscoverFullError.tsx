import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button, Text, spacing } from '@shared/ui';

import { describeError } from '@shared/lib/describeError';
import { SEARCH_UNAVAILABLE_TITLE } from '../state';

export function DiscoverUnavailable(): ReactElement {
  return (
    <View testID="discover-unavailable" style={styles.center}>
      <Text variant="title">{SEARCH_UNAVAILABLE_TITLE}</Text>
      <Text variant="label" tone="secondary" style={styles.sub}>
        Check back in a little while.
      </Text>
    </View>
  );
}

interface FullErrorProps {
  error: unknown;
  onRetry: () => void;
}

export function DiscoverFullError({ error, onRetry }: FullErrorProps): ReactElement {
  const { title, body } = describeError(error);
  return (
    <View testID="discover-full-error" style={styles.center}>
      <Text variant="title">{title}</Text>
      <Text variant="label" tone="secondary" style={styles.sub}>
        {body}
      </Text>
      <Button testID="discover-retry" label="Retry" onPress={onRetry} />
    </View>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  sub: { marginTop: spacing.xs, marginBottom: spacing.lg },
});
