import { StyleSheet, View } from 'react-native';
import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

import { useOAuth } from '../hooks/useOAuth';

const OAUTH_ERROR = "Couldn't sign in with that provider. Please try again.";

/**
 * One-tap social sign-in (AC#10). Google only for now — Sign in with Apple is
 * deferred because it needs a paid Apple Developer account, and App Store
 * Guideline 4.8 (which would force Apple alongside Google) cannot trigger
 * without App Store distribution, which also needs that account. `useOAuth`
 * stays provider-agnostic, so adding an Apple button later is one line.
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
