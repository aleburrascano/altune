import { type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import { sharedStyles } from './styles';

export interface SectionErrorProps {
  /** Derives `${testIDPrefix}-error` (container) and `${testIDPrefix}-retry` (button). */
  testIDPrefix: string;
  message: string;
  onRetry: () => void;
}

/**
 * The one "couldn't load X" + Retry block for detail lists. Tone, layout and
 * testIDs are fixed here so no call site can drift to a muted tone or drop the
 * testID pair its siblings carry; callers supply only the copy and the retry.
 */
export function SectionError({ testIDPrefix, message, onRetry }: SectionErrorProps): ReactElement {
  return (
    <View testID={`${testIDPrefix}-error`} style={styles.container}>
      <Text variant="body" tone="danger">
        {message}
      </Text>
      <Button
        testID={`${testIDPrefix}-retry`}
        label="Retry"
        onPress={onRetry}
        style={sharedStyles.retryButton}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { alignItems: 'center', paddingVertical: spacing.md },
});
