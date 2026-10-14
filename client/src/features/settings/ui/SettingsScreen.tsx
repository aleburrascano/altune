import { ChevronRight, User } from 'lucide-react-native';
import type { ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { appVersionLabel } from '@shared/device/device';
import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { useSignOut } from '@shared/auth/useSignOut';
import { offlineDownloadsSupported } from '@shared/offline/offlineSupport';
import { useAccountEmail } from '@features/settings/hooks/useAccountEmail';
import { useSettingsClearSearchHistory } from '@features/settings/hooks/useSettingsClearSearchHistory';
import { useDownloadStats, useRemoveDownloads } from '@features/settings/hooks/useDownloads';
import { AnalyticsCard } from '@features/settings/ui/AnalyticsCard';
import { AppearanceCard } from '@features/settings/ui/AppearanceCard';
import { DangerZoneCard } from '@features/settings/ui/DangerZoneCard';
import { LibraryCard } from '@features/settings/ui/LibraryCard';
import { OfflineDownloadsCard } from '@features/settings/ui/OfflineDownloadsCard';
import { SettingsCard, SettingsRow } from '@features/settings/ui/SettingsCard';

export function SettingsScreen(): ReactElement {
  const theme = useTheme();
  const email = useAccountEmail();
  const { state: signOutState, signOut } = useSignOut();
  const clearHistory = useSettingsClearSearchHistory();
  const stats = useDownloadStats();
  const downloads = useRemoveDownloads(stats);

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.content} showsVerticalScrollIndicator={false}>
        <Text variant="displayL" style={styles.title}>
          Settings
        </Text>

        <SettingsCard>
          <SettingsRow
            testID="settings-account"
            first
            icon={User}
            tone="accent"
            label={email || 'Not signed in'}
            detail="Account"
            right={<ChevronRight size={16} color={theme.color.textTertiary} />}
          />
        </SettingsCard>

        <AppearanceCard />

        <AnalyticsCard />

        {offlineDownloadsSupported && <OfflineDownloadsCard stats={stats} />}

        <LibraryCard />

        <DangerZoneCard
          downloads={downloads}
          signOutState={signOutState}
          clearHistory={clearHistory}
          signOut={signOut}
        />

        <View style={styles.footer}>
          <Text testID="settings-version" variant="caption" tone="tertiary">
            Altune {appVersionLabel()}
          </Text>
        </View>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  content: { paddingBottom: spacing['3xl'] },
  title: { paddingTop: spacing.sm, paddingBottom: spacing.sm },
  footer: { alignItems: 'center', paddingTop: spacing['3xl'] },
});
