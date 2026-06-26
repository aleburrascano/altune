import { Pressable, StyleSheet, View } from 'react-native';
import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import { useOAuth } from '../hooks/useOAuth';
import { GoogleLogo } from './hero/GoogleLogo';

const OAUTH_ERROR = "Couldn't sign in with that provider. Please try again.";

/**
 * One-tap social sign-in (AC#10) — Google only for now (Apple deferred, see
 * ADR-0018). A compact, auto-width pill with the official Google logo, under
 * an "or" divider. `useOAuth` is provider-agnostic so Apple is a one-pill add.
 */
export function OAuthButtons(): ReactElement {
  const theme = useTheme();
  const { state, signInWith } = useOAuth();
  const pending = state.kind === 'pending';

  return (
    <View style={styles.wrap}>
      <View style={styles.dividerRow}>
        <View style={[styles.rule, { backgroundColor: theme.color.border }]} />
        <Text variant="caption" tone="tertiary">
          or
        </Text>
        <View style={[styles.rule, { backgroundColor: theme.color.border }]} />
      </View>
      <View style={styles.pillRow}>
        <Pressable
          testID="oauth-google"
          onPress={() => void signInWith('google')}
          disabled={pending}
          accessibilityRole="button"
          accessibilityLabel="Continue with Google"
          accessibilityState={{ disabled: pending }}
          style={({ pressed }) => [
            styles.pill,
            {
              borderColor: theme.color.border,
              backgroundColor: pressed ? theme.color.surface2 : theme.color.surface1,
              opacity: pending ? 0.5 : 1,
            },
          ]}
        >
          <GoogleLogo size={16} />
          <Text variant="label">Google</Text>
        </Pressable>
      </View>
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
  pillRow: { alignItems: 'center' },
  pill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.full,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.lg,
  },
});
