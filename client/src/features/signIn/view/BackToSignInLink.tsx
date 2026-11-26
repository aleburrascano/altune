import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { AppLink } from '@shared/navigation';
import { Text, spacing } from '@shared/ui';

export function BackToSignInLink({
  testID = 'back-to-sign-in',
}: {
  testID?: string;
}): ReactElement {
  return (
    <View style={styles.linkWrap}>
      <AppLink href="/sign-in" testID={testID}>
        <Text variant="label" tone="accent">
          Back to sign in
        </Text>
      </AppLink>
    </View>
  );
}

const styles = StyleSheet.create({
  linkWrap: { alignItems: 'center', paddingTop: spacing.sm },
});
