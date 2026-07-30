import Constants from 'expo-constants';
import {
  ChevronRight,
  DownloadCloud,
  Eraser,
  LogOut,
  Moon,
  Sparkles,
  Trash2,
  User,
} from 'lucide-react-native';
import { useState, type ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';
import { useThemePreference } from '@shared/ui/theme/themePreference';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { formatBytes, pinnedBytes } from '@shared/offline/pinnedFiles';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { ConfirmDialog } from './ConfirmDialog';
import { FeedbackCard } from './FeedbackCard';
import { ReportIssueDialog } from './ReportIssueDialog';
import { SettingsCard } from './SettingsCard';
import { SettingsRow } from './SettingsRow';
import { ThemeSegment } from './ThemeSegment';

type Confirmation = 'downloads' | 'history' | 'sign-out';

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
  const [confirming, setConfirming] = useState<Confirmation | null>(null);

  const downloadCount = Object.values(pinnedEntries).filter((e) => e.status === 'ready').length;
  const downloadSize = formatBytes(pinnedBytes());
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
            detail={
              scheme === 'light'
                ? 'Light mode has no design pass yet (ADR-0008)'
                : undefined
            }
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
                : `${downloadCount} ${downloadCount === 1 ? 'track' : 'tracks'}`
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

        <SettingsCard label="Danger zone" danger>
          {downloadCount > 0 ? (
            <SettingsRow
              testID="settings-remove-downloads"
              first
              icon={Trash2}
              tone="danger"
              label="Remove all downloads"
              detail={`Frees ${downloadSize} · tracks stay in your library`}
              onPress={() => setConfirming('downloads')}
            />
          ) : null}
          <SettingsRow
            testID="settings-clear-search-history"
            first={downloadCount === 0}
            icon={Eraser}
            tone="danger"
            label="Clear search history"
            onPress={() => setConfirming('history')}
            disabled={clearHistory.isPending}
            right={
              clearHistory.isSuccess ? (
                <Text variant="label" tone="success">
                  Cleared
                </Text>
              ) : null
            }
          />
          <SettingsRow
            testID="settings-sign-out"
            icon={LogOut}
            tone="danger"
            label="Sign out"
            onPress={() => setConfirming('sign-out')}
            disabled={signOutState.kind === 'pending'}
          />
        </SettingsCard>

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

      <ConfirmDialog
        testID="settings-confirm-remove-downloads"
        visible={confirming === 'downloads'}
        icon={Trash2}
        title="Remove all downloads?"
        body={`${downloadCount} ${downloadCount === 1 ? 'track' : 'tracks'} (${downloadSize}) will be deleted from this device. They stay in your library and can be downloaded again.`}
        confirmLabel="Remove"
        onConfirm={unpinAll}
        onClose={() => setConfirming(null)}
      />

      <ConfirmDialog
        testID="settings-confirm-clear-history"
        visible={confirming === 'history'}
        icon={Eraser}
        title="Clear search history?"
        body="Your recent searches will be deleted from this device and the server."
        confirmLabel="Clear"
        onConfirm={() => clearHistory.mutate()}
        onClose={() => setConfirming(null)}
      />

      <ConfirmDialog
        testID="settings-confirm-sign-out"
        visible={confirming === 'sign-out'}
        icon={LogOut}
        title="Sign out?"
        body="Your library stays on the server. Downloads on this device are kept."
        confirmLabel="Sign out"
        onConfirm={() => void signOut()}
        onClose={() => setConfirming(null)}
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
