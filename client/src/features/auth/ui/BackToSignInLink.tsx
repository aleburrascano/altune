import { Link } from 'expo-router';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

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
