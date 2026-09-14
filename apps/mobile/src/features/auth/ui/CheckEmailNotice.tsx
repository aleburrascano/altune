import { StyleSheet, View } from 'react-native';
import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import { BackToSignInLink } from './BackToSignInLink';
import { AuthHeroLayout } from './hero/AuthHeroLayout';

export function CheckEmailNotice(): ReactElement {
  return (
    <AuthHeroLayout testID="check-email-screen" tagline="Almost there." background={false}>
      <View style={styles.form}>
        <Text variant="title">Check your email</Text>
        <Banner testID="check-email-banner" tone="info">
          We&apos;ve sent you a link to confirm your account. Open it on this device to finish
          signing up.
        </Banner>
        <BackToSignInLink testID="link-to-sign-in" />
      </View>
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.md },
});
