import { Link } from 'expo-router';
import { StyleSheet, View } from 'react-native';
import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import { AuthHeroLayout } from './hero/AuthHeroLayout';

/**
 * Post-sign-up "check your email" state (AC#4). Shown for BOTH a fresh
 * sign-up and an already-registered address (AC#5) — the copy never reveals
 * which, so it stays anti-enumeration-safe.
 */
export function CheckEmailNotice(): ReactElement {
  return (
    <AuthHeroLayout testID="check-email-screen" tagline="Almost there.">
      <View style={styles.form}>
        <Text variant="title">Check your email</Text>
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
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.md },
  linkWrap: { alignItems: 'center', paddingTop: spacing.sm },
});
