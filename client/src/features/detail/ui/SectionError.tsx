import { type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import type { ContentFailure } from '@features/detail/domain/content-status';

import { sharedStyles } from '@features/detail/ui/sharedStyles';

export interface SectionErrorProps {
  testIDPrefix: string;
  message: string;
  onRetry: () => void;
  failure?: ContentFailure | null;
}

const SETTLED_NOTE = 'Not available right now — try again later.';

export function SectionError({
  testIDPrefix,
  message,
  onRetry,
  failure,
}: SectionErrorProps): ReactElement {
  return (
    <View testID={`${testIDPrefix}-error`} style={styles.container}>
      <Text variant="body" tone="danger">
        {message}
      </Text>
      {failure === 'settled' ? (
        <Text
          testID={`${testIDPrefix}-settled`}
          variant="caption"
          tone="tertiary"
          style={styles.settledNote}
        >
          {SETTLED_NOTE}
        </Text>
      ) : (
        <Button
          action="detail.section.retry"
          testID={`${testIDPrefix}-retry`}
          label="Retry"
          onPress={onRetry}
          style={sharedStyles.retryButton}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { alignItems: 'center', paddingVertical: spacing.md },
  settledNote: { marginTop: spacing.xs, textAlign: 'center' },
});
