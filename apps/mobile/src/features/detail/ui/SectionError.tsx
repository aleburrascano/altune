import { type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import type { ContentFailure } from '../content-status';

import { sharedStyles } from './styles';

export interface SectionErrorProps {
  /**
   * Derives `${testIDPrefix}-error` (container), `${testIDPrefix}-retry`
   * (button) and `${testIDPrefix}-settled` (the no-retry note).
   */
  testIDPrefix: string;
  message: string;
  onRetry: () => void;
  /** An unclassified failure keeps the retry: a cause we cannot read may still pass on a second ask. */
  failure?: ContentFailure | null;
}

const SETTLED_NOTE = 'Not available right now — try again later.';

/**
 * The one "couldn't load X" block for detail lists. Tone, layout and testIDs
 * are fixed here so no call site can drift to a muted tone or drop the testID
 * pair its siblings carry; callers supply only the copy and the retry.
 *
 * A settled failure offers no Retry, because the server has already decided
 * this request and tapping would deterministically fail again.
 */
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
