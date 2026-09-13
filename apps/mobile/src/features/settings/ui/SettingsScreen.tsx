import Constants from 'expo-constants';
import { ChevronRight, DownloadCloud, Moon, Sparkles, User } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';
import { useThemePreference } from '@shared/ui/theme/themePreference';
import { formatBytes, pinnedByteTotal, usePinnedStore } from '@shared/offline/pinnedStore';
import { countLabel } from '@shared/lib/format';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { DangerZoneCard } from './DangerZoneCard';
import { FeedbackCard } from './FeedbackCard';
import { ReportIssueDialog } from './ReportIssueDialog';
import { SettingsCard } from './SettingsCard';
import { SettingsRow } from './SettingsRow';
import { ThemeSegment } from './ThemeSegment';

export function SettingsScreen(): ReactElement {
  const theme = useTheme();
  const sessionState = useSession();
  const { state: signOutState, signOut } = useSignOut();
  const backfill = useBackfillFeatured();
  const clearHistory = useClearSearchHistory();
  const scheme = useThemePreference((s) => s.scheme);
  const setScheme = useThemePreference((s) => s.setScheme);
  const pinnedEntries = usePinnedStore((s) => s.entries);
  const unpinAll = usePinnedStore((s) => s.unpinAll);

  const [reporting, setReporting] = useState(false);

  const downloadCount = Object.values(pinnedEntries).filter((e) => e.status === 'ready').length;
  const downloadSize = formatBytes(pinnedByteTotal());
  const email = sessionState.status === 'signed-in' ? (sessionState.session.user.email ?? '') : '';

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
            label={
              downloadCount === 0
                ? 'No downloads on this device'
                : `${downloadCount} ${countLabel(downloadCount, 'track')}`
            }
            detail={downloadCount === 0 ? undefined : downloadSize}
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
              <Text variant="label" tone={backfill.isSuccess ? 'success' : 'accent'}>
                {backfill.isPending ? 'Running…' : backfill.isSuccess ? 'Done' : 'Run'}
              </Text>
            }
          />
        </SettingsCard>

        <DangerZoneCard
          downloadCount={downloadCount}
          downloadSize={downloadSize}
          signOutState={signOutState}
          clearHistory={clearHistory}
          unpinAll={unpinAll}
          signOut={signOut}
        />

        <View style={styles.footer}>
          <Text testID="settings-version" variant="caption" tone="tertiary">
            Altune {appVersion}
          </Text>
        </View>
      </ScrollView>

      <ReportIssueDialog
        visible={reporting}
        onClose={() => setReporting(false)}
        screen="settings"
      />
    </Screen>
  );
}

type BackfillState = {
  isPending: boolean;
  data: { updated: number; scanned: number } | undefined;
};

function backfillDetail(backfill: BackfillState): string | undefined {
  if (backfill.isPending) return 'Resolving featured artists…';
  if (backfill.data == null) return undefined;
  return `Updated ${backfill.data.updated} of ${backfill.data.scanned} tracks`;
}

const appVersion: string = Constants.expoConfig?.version ?? 'dev';

const styles = StyleSheet.create({
  content: { paddingBottom: spacing['3xl'] },
  title: { paddingTop: spacing.sm, paddingBottom: spacing.sm },
  footer: { alignItems: 'center', paddingTop: spacing['3xl'] },
});
