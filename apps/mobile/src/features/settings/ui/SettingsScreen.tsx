import Constants from 'expo-constants';
import { ChevronRight, User } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { useSignOut } from '@shared/auth/useSignOut';
import { offlineDownloadsSupported } from '@shared/offline/offlineSupport';
import { useAccountEmail } from '../hooks/useAccountEmail';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useDownloadStats } from '../hooks/useDownloadStats';
import { useRemoveDownloads } from '../hooks/useRemoveDownloads';
import { AppearanceCard } from './AppearanceCard';
import { DangerZoneCard } from './DangerZoneCard';
import { FeedbackCard } from './FeedbackCard';
import { LibraryCard } from './LibraryCard';
import { OfflineDownloadsCard } from './OfflineDownloadsCard';
import { ReportIssueModal } from './ReportIssueModal';
import { SettingsCard } from './SettingsCard';
import { SettingsRow } from './SettingsRow';

export function SettingsScreen(): ReactElement {
  const theme = useTheme();
  const email = useAccountEmail();
  const { state: signOutState, signOut } = useSignOut();
  const clearHistory = useClearSearchHistory();
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
            Altune {appVersion}
          </Text>
        </View>
      </ScrollView>

      <ReportIssueModal visible={reporting} onClose={() => setReporting(false)} screen="settings" />
    </Screen>
  );
}

const appVersion: string = Constants.expoConfig?.version ?? 'dev';

const styles = StyleSheet.create({
  content: { paddingBottom: spacing['3xl'] },
  title: { paddingTop: spacing.sm, paddingBottom: spacing.sm },
  footer: { alignItems: 'center', paddingTop: spacing['3xl'] },
});
