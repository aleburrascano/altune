import { ChevronRight, User } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { appVersion } from '@shared/device/device';
import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { useSignOut } from '@shared/auth/useSignOut';
import { offlineDownloadsSupported } from '@shared/offline/offlineSupport';
import { useAccountEmail } from '@features/settings/hooks/useAccountEmail';
import { useSettingsClearSearchHistory } from '@features/settings/hooks/useClearSearchHistory';
import { useDownloadStats } from '@features/settings/hooks/useDownloadStats';
import { useRemoveDownloads } from '@features/settings/hooks/useRemoveDownloads';
import { AppearanceCard } from '@features/settings/ui/AppearanceCard';
import { DangerZoneCard } from '@features/settings/ui/DangerZoneCard';
import { FeedbackCard } from '@features/settings/ui/FeedbackCard';
import { LibraryCard } from '@features/settings/ui/LibraryCard';
import { OfflineDownloadsCard } from '@features/settings/ui/OfflineDownloadsCard';
import { ReportIssueModal } from '@features/settings/ui/ReportIssueModal';
import { SettingsCard } from '@features/settings/ui/SettingsCard';
import { SettingsRow } from '@features/settings/ui/SettingsRow';

export function SettingsScreen(): ReactElement {
  const theme = useTheme();
  const email = useAccountEmail();
  const { state: signOutState, signOut } = useSignOut();
  const clearHistory = useSettingsClearSearchHistory();
  const stats = useDownloadStats();
  const downloads = useRemoveDownloads(stats);

  const [reporting, setReporting] = useState(false);

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

        <FeedbackCard onPress={() => setReporting(true)} />

        <AppearanceCard />

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
            Altune {appVersion()}
          </Text>
        </View>
      </ScrollView>

      <ReportIssueModal visible={reporting} onClose={() => setReporting(false)} screen="settings" />
    </Screen>
  );
}

const styles = StyleSheet.create({
  content: { paddingBottom: spacing['3xl'] },
  title: { paddingTop: spacing.sm, paddingBottom: spacing.sm },
  footer: { alignItems: 'center', paddingTop: spacing['3xl'] },
});
