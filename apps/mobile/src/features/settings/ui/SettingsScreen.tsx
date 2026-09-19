import Constants from 'expo-constants';
import { ChevronRight, DownloadCloud, Moon, Sparkles, User } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { useSignOut } from '@shared/auth/useSignOut';
import { useThemePreference } from '@shared/ui/theme/themePreference';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { backfillActionLabel, backfillActionTone, backfillDetail } from '../hooks/backfillStatus';
import { useAccountEmail } from '../hooks/useAccountEmail';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useDownloadStats } from '../hooks/useDownloadStats';
import { DangerZoneCard } from './DangerZoneCard';
import { FeedbackCard } from './FeedbackCard';
import { ReportIssueModal } from './ReportIssueModal';
import { SettingsCard } from './SettingsCard';
import { SettingsRow } from './SettingsRow';
import { ThemeSegment } from './ThemeSegment';

export function SettingsScreen(): ReactElement {
  const theme = useTheme();
  const email = useAccountEmail();
  const { state: signOutState, signOut } = useSignOut();
  const backfill = useBackfillFeatured();
  const clearHistory = useClearSearchHistory();
  const scheme = useThemePreference((s) => s.scheme);
  const setScheme = useThemePreference((s) => s.setScheme);
  const { downloadCount, downloadBytes, downloadSize, usageLabel, usageDetail } =
    useDownloadStats();
  const unpinAll = usePinnedStore((s) => s.unpinAll);
  const lastUnpinAll = usePinnedStore((s) => s.lastUnpinAll);

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

        <SettingsCard label="Appearance">
          <SettingsRow
            first
            icon={Moon}
            label="Theme"
            detail={scheme === 'light' ? 'Light mode has no design pass yet (ADR-0008)' : undefined}
            right={<ThemeSegment scheme={scheme} onSelect={setScheme} />}
          />
        </SettingsCard>

        <SettingsCard label="Offline downloads">
          <SettingsRow
            testID="settings-downloads-usage"
            first
            icon={DownloadCloud}
            tone={downloadCount > 0 ? 'success' : 'neutral'}
            label={usageLabel}
            detail={usageDetail}
          />
        </SettingsCard>

        <SettingsCard label="Library">
          <SettingsRow
            testID="settings-backfill-featured"
            first
            icon={Sparkles}
            tone="warning"
            label="Resolve featured artists"
            detail={backfillDetail(backfill)}
            onPress={() => backfill.mutate()}
            disabled={backfill.isPending}
            right={
              <Text variant="label" tone={backfillActionTone(backfill)}>
                {backfillActionLabel(backfill)}
              </Text>
            }
          />
        </SettingsCard>

        <DangerZoneCard
          downloadCount={downloadCount}
          downloadBytes={downloadBytes}
          downloadSize={downloadSize}
          signOutState={signOutState}
          clearHistory={clearHistory}
          lastUnpinAll={lastUnpinAll}
          unpinAll={unpinAll}
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
