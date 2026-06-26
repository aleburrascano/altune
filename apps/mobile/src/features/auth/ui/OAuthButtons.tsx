import { StyleSheet, View } from 'react-native';
import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

import { useOAuth } from '../hooks/useOAuth';

const OAUTH_ERROR = "Couldn't sign in with that provider. Please try again.";

/**
 * Apple + Google one-tap sign-in, rendered on both auth screens (AC#10).
 * Apple ships alongside Google by necessity (App Store Guideline 4.8).
 */
export function OAuthButtons(): ReactElement {
  const theme = useTheme();
  const { state, signInWith } = useOAuth();
  const pendingProvider = state.kind === 'pending' ? state.provider : null;

  return (
    <View style={styles.wrap}>
      <View style={styles.dividerRow}>
        <View style={[styles.rule, { backgroundColor: theme.color.border }]} />
        <Text variant="caption" tone="tertiary">
          or
        </Text>
        <View style={[styles.rule, { backgroundColor: theme.color.border }]} />
      </View>
      <Button
        testID="oauth-apple"
        label="Continue with Apple"
        variant="secondary"
        onPress={() => void signInWith('apple')}
        loading={pendingProvider === 'apple'}
        disabled={state.kind === 'pending'}
      />
      <Button
        testID="oauth-google"
        label="Continue with Google"
        variant="secondary"
        onPress={() => void signInWith('google')}
        loading={pendingProvider === 'google'}
        disabled={state.kind === 'pending'}
      />
      {state.kind === 'error' ? (
        <Banner testID="oauth-error" tone="danger">
          {OAUTH_ERROR}
        </Banner>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: spacing.md },
  dividerRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  rule: { flex: 1, height: StyleSheet.hairlineWidth },
});
