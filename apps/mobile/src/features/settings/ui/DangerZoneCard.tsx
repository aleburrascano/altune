import { Eraser, LogOut, Trash2 } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';

import { Text } from '@shared/ui';
import type { SignOutResult } from '@shared/auth/useSignOut';
import { countLabel } from '@shared/lib/format';
import type { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { ConfirmDialog } from './ConfirmDialog';
import { SettingsCard } from './SettingsCard';
import { SettingsRow } from './SettingsRow';

type Confirmation = 'downloads' | 'history' | 'sign-out';

type DangerZoneCardProps = {
  downloadCount: number;
  downloadSize: string;
  signOutState: SignOutResult;
  clearHistory: ReturnType<typeof useClearSearchHistory>;
  unpinAll: () => void;
  signOut: () => Promise<void>;
};

export function DangerZoneCard({
  downloadCount,
  downloadSize,
  signOutState,
  clearHistory,
  unpinAll,
  signOut,
}: DangerZoneCardProps): ReactElement {
  const [confirming, setConfirming] = useState<Confirmation | null>(null);

  return (
    <>
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

      <ConfirmDialog
        testID="settings-confirm-remove-downloads"
        visible={confirming === 'downloads'}
        icon={Trash2}
        title="Remove all downloads?"
        body={`${downloadCount} ${countLabel(downloadCount, 'track')} (${downloadSize}) will be deleted from this device. They stay in your library and can be downloaded again.`}
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
    </>
  );
}
