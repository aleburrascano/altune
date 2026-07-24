import type { ReactElement } from 'react';
import { Alert, StyleSheet, View } from 'react-native';
import Constants from 'expo-constants';

import { Button, Screen, Text, spacing, useTheme } from '@shared/ui';
import { useSession } from '@shared/auth/useSession';
import { useThemePreference } from '@shared/ui/theme/themePreference';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { formatBytes, pinnedBytes } from '@shared/offline/pinnedFiles';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useSignOut } from '@shared/auth/useSignOut';

export function SettingsScreen(): ReactElement {
  const theme = useTheme();
  const sessionState = useSession();
  const { state: signOutState, signOut } = useSignOut();
  const isPending = signOutState.kind === 'pending';
  const backfill = useBackfillFeatured();
  const clearHistory = useClearSearchHistory();
  const scheme = useThemePreference((s) => s.scheme);
  const toggleScheme = useThemePreference((s) => s.toggle);
  const pinnedEntries = usePinnedStore((s) => s.entries);
  const unpinAll = usePinnedStore((s) => s.unpinAll);

  const downloadCount = Object.values(pinnedEntries).filter((e) => e.status === 'ready').length;
  // Read from disk rather than summing the index: the number people check is
  // the space actually used, and the two can drift.
  const downloadSize = formatBytes(pinnedBytes());

  const confirmRemoveDownloads = (): void => {
    Alert.alert(
      'Remove all downloads',
      `${downloadCount} ${downloadCount === 1 ? 'track' : 'tracks'} (${downloadSize}) will be deleted from this device. They stay in your library.`,
      [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Remove', style: 'destructive', onPress: unpinAll },
      ],
    );
  };

  const confirmClearHistory = (): void => {
    Alert.alert('Clear search history', 'Your recent searches will be deleted.', [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Clear', style: 'destructive', onPress: () => clearHistory.mutate() },
    ]);
  };

  const clearHistoryLabel = clearHistory.isPending
    ? 'Clearing…'
    : clearHistory.isSuccess
      ? 'Search history cleared'
      : 'Clear search history';

  const backfillLabel = backfill.isPending
    ? 'Resolving featured artists…'
    : backfill.isSuccess
      ? `Updated ${backfill.data.updated} of ${backfill.data.scanned} tracks`
      : 'Resolve featured artists';

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
        style={styles.action}
      />

      <Text variant="caption" tone="tertiary" style={styles.sectionLabel}>
        Library maintenance
      </Text>
      <Button
        testID="settings-backfill-featured"
        label={backfillLabel}
        variant="ghost"
        loading={backfill.isPending}
        onPress={() => { backfill.mutate(); }}
        style={styles.action}
      />

      <Text variant="caption" tone="tertiary" style={styles.sectionLabel}>
        Offline downloads
      </Text>
      <Text testID="settings-downloads-usage" variant="label" tone="secondary">
        {downloadCount === 0
          ? 'No downloads on this device'
          : `${downloadCount} ${downloadCount === 1 ? 'track' : 'tracks'} · ${downloadSize}`}
      </Text>
      {downloadCount > 0 ? (
        <Button
          testID="settings-remove-downloads"
          label="Remove all downloads"
          variant="ghost"
          onPress={confirmRemoveDownloads}
          style={styles.action}
        />
      ) : null}

      <Text variant="caption" tone="tertiary" style={styles.sectionLabel}>
        Appearance
      </Text>
      <Button
        testID="settings-theme-toggle"
        label={scheme === 'dark' ? 'Theme: Dark' : 'Theme: Light'}
        variant="ghost"
        onPress={toggleScheme}
        style={styles.action}
      />
      {scheme === 'light' ? (
        <Text variant="caption" tone="tertiary" style={styles.note}>
          Light mode hasn&apos;t had a design pass yet (ADR-0008) — some screens
          will look rough.
        </Text>
      ) : null}

      <Text variant="caption" tone="tertiary" style={styles.sectionLabel}>
        Privacy
      </Text>
      <Button
        testID="settings-clear-search-history"
        label={clearHistoryLabel}
        variant="ghost"
        loading={clearHistory.isPending}
        onPress={confirmClearHistory}
        style={styles.action}
      />

      <View style={styles.spacer} />
      <Text testID="settings-version" variant="caption" tone="tertiary" style={styles.version}>
        Altune {appVersion}
      </Text>
    </Screen>
  );
}

// The version users can read back during a bug report. `expo-constants` exposes
// the manifest's version at runtime, so it tracks app.json without a second
// place to bump.
const appVersion: string = Constants.expoConfig?.version ?? 'dev';

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
  action: { alignSelf: 'flex-start' },
  sectionLabel: { marginTop: spacing.xl, marginBottom: spacing.xs },
  spacer: { flex: 1, minHeight: spacing.xl },
  note: { marginTop: spacing.xs, maxWidth: 320 },
  version: { textAlign: 'center', paddingBottom: spacing.lg },
});
