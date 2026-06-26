import { Link } from 'expo-router';
import { StyleSheet, View } from 'react-native';
import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { spacing } from '@shared/ui/theme';

/**
 * Post-sign-up "check your email" state (AC#4). Shown for BOTH a fresh
 * sign-up and an already-registered address (AC#5) — the copy never reveals
 * which, so it stays anti-enumeration-safe.
 */
export function CheckEmailNotice(): ReactElement {
  return (
    <Screen testID="check-email-screen">
      <View style={styles.body}>
        <View style={styles.header}>
          <Wordmark size={40} />
          <Text variant="title">Check your email</Text>
        </View>
        <Banner testID="check-email-banner" tone="info">
          We&apos;ve sent you a link to confirm your account. Open it on this device to finish
          signing up.
        </Banner>
        <View style={styles.linkWrap}>
          <Link href="/sign-in" testID="link-to-sign-in">
            <Text variant="label" tone="accent">
              Back to sign in
            </Text>
          </Link>
        </View>
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { flex: 1, justifyContent: 'center', gap: spacing.md },
  header: { alignItems: 'center', gap: spacing.sm, marginBottom: spacing.xl },
  linkWrap: { alignItems: 'center', paddingVertical: spacing.sm },
});
