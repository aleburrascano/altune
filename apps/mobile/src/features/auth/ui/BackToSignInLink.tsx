import { Link } from 'expo-router';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

/**
 * Renders the shared centered "Back to sign in" link that returns to the
 * sign-in screen. Callers pass `testID` to preserve their existing selector.
 */
export function BackToSignInLink({
  testID = 'back-to-sign-in',
}: {
  testID?: string;
}): ReactElement {
  return (
    <View style={styles.linkWrap}>
      <Link href="/sign-in" testID={testID}>
        <Text variant="label" tone="accent">
          Back to sign in
        </Text>
      </Link>
    </View>
  );
}

const styles = StyleSheet.create({
  linkWrap: { alignItems: 'center', paddingTop: spacing.sm },
});
