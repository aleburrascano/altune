import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button, Screen, Text, spacing, useTheme } from '@shared/ui';
import { useSession } from '../../auth/hooks/useSession';
import { useSignOut } from '@shared/auth/useSignOut';

export function SettingsScreen(): ReactElement {
  const theme = useTheme();
  const sessionState = useSession();
  const { state: signOutState, signOut } = useSignOut();
  const isPending = signOutState.kind === 'pending';

  const email =
    sessionState.status === 'signed-in' ? (sessionState.session.user.email ?? '') : '';
  const initial = email.length > 0 ? email[0]!.toUpperCase() : '?';

  return (
    <Screen>
      <Text variant="displayL" style={styles.title}>Settings</Text>

      <View style={styles.profileCard}>
        <View style={[styles.avatar, { backgroundColor: theme.color.accent }]}>
          <Text variant="displayL" tone="onAccent">{initial}</Text>
        </View>
        <View style={styles.profileInfo}>
          <Text variant="bodyStrong">{email || 'Not signed in'}</Text>
          <Text variant="caption" tone="secondary">Account</Text>
        </View>
      </View>

      <View style={[styles.divider, { backgroundColor: theme.color.border }]} />

      <Button
        testID="settings-sign-out"
        label={isPending ? 'Signing out…' : 'Sign Out'}
        variant="ghost"
        loading={isPending}
        onPress={() => { void signOut(); }}
        style={styles.signOutBtn}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  title: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
  profileCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.lg,
    paddingVertical: spacing.lg,
  },
  avatar: {
    width: 56,
    height: 56,
    borderRadius: 28,
    alignItems: 'center',
    justifyContent: 'center',
  },
  profileInfo: { flex: 1, gap: spacing.xs },
  divider: { height: StyleSheet.hairlineWidth, marginVertical: spacing.lg },
  signOutBtn: { alignSelf: 'flex-start' },
});
