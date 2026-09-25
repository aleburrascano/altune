import { Eraser, LogOut, Trash2, type LucideIcon } from 'lucide-react-native';

import type { SignOutResult } from '@shared/auth/useSignOut';
import { countLabel } from '@shared/lib/format';
import type { UnpinAllOutcome } from '@shared/offline/pinnedStore';
import type { TextTone } from '@shared/ui/primitives/Text';
import { failureCopyForAction } from '../failureCopyForAction';
import type { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { hasNoDownloads } from '../hooks/useDownloadStats';

// Closed on purpose: the open confirm is chosen by comparing against this key,
// so a value outside the set would match no confirm and open nothing.
export type DangerZoneActionKey = 'downloads' | 'history' | 'sign-out';

// One destructive action: the row that opens it and the confirm that runs it.
// The row and its confirm share the icon.
type DangerZoneAction = {
  key: DangerZoneActionKey;
  icon: LucideIcon;
  row: {
    testID: string;
    label: string;
    detail?: string;
    disabled?: boolean;
    // Short outcome label shown on the row's right edge.
    status?: { label: string; tone: Extract<TextTone, 'success' | 'danger'> };
    // Hides only the row; the confirm stays mounted so an open one is not
    // torn down (and later resurrected) when the row disappears.
    hidden?: boolean;
  };
  confirm: {
    testID: string;
    title: string;
    body: string;
    confirmLabel: string;
    onConfirm: () => void;
  };
};

function removeDownloadsBody(downloadCount: number, downloadSize: string): string {
  if (downloadCount === 0) {
    return `Leftover download files (${downloadSize}) will be deleted from this device.`;
  }
  return `${downloadCount} ${countLabel(downloadCount, 'track')} (${downloadSize}) will be deleted from this device. They stay in your library and can be downloaded again.`;
}

// A remove-all that cleared everything hides the row, so only the partial pass has a state to show.
function removeDownloadsOutcome(
  lastUnpinAll: UnpinAllOutcome | undefined,
): Pick<DangerZoneAction['row'], 'detail' | 'status'> {
  if (lastUnpinAll !== 'partial') return {};
  return {
    detail: "Some downloads couldn't be removed — try again.",
    status: { label: 'Failed', tone: 'danger' },
  };
}

function removeDownloadsAction(opts: {
  downloadCount: number;
  downloadBytes: number;
  downloadSize: string;
  lastUnpinAll?: UnpinAllOutcome | undefined;
  unpinAll: () => void;
}): DangerZoneAction {
  const { downloadCount, downloadSize } = opts;
  return {
    key: 'downloads',
    icon: Trash2,
    row: {
      testID: 'settings-remove-downloads',
      label: 'Remove all downloads',
      detail: `Frees ${downloadSize} · tracks stay in your library`,
      hidden: hasNoDownloads(downloadCount, opts.downloadBytes),
      ...removeDownloadsOutcome(opts.lastUnpinAll),
    },
    confirm: {
      testID: 'settings-confirm-remove-downloads',
      title: 'Remove all downloads?',
      body: removeDownloadsBody(downloadCount, downloadSize),
      confirmLabel: 'Remove',
      onConfirm: opts.unpinAll,
    },
  };
}

function clearHistoryOutcome(
  clearHistory: ReturnType<typeof useClearSearchHistory>,
): Pick<DangerZoneAction['row'], 'detail' | 'status'> {
  if (clearHistory.isError) {
    return {
      detail: failureCopyForAction(clearHistory.error),
      status: { label: 'Failed', tone: 'danger' },
    };
  }
  return clearHistory.isSuccess ? { status: { label: 'Cleared', tone: 'success' } } : {};
}

function clearSearchHistoryAction(
  clearHistory: ReturnType<typeof useClearSearchHistory>,
): DangerZoneAction {
  return {
    key: 'history',
    icon: Eraser,
    row: {
      testID: 'settings-clear-search-history',
      label: 'Clear search history',
      disabled: clearHistory.isPending,
      ...clearHistoryOutcome(clearHistory),
    },
    confirm: {
      testID: 'settings-confirm-clear-history',
      title: 'Clear search history?',
      body: 'Your recent searches will be deleted from this device and the server.',
      confirmLabel: 'Clear',
      onConfirm: () => clearHistory.mutate(),
    },
  };
}

function signOutAction(opts: {
  signOutState: SignOutResult;
  signOut: () => Promise<void>;
}): DangerZoneAction {
  const { signOutState } = opts;
  return {
    key: 'sign-out',
    icon: LogOut,
    row: {
      testID: 'settings-sign-out',
      label: 'Sign out',
      disabled: signOutState.status === 'loading',
      ...(signOutState.status === 'error'
        ? {
            detail: failureCopyForAction(signOutState.error),
            status: { label: 'Failed', tone: 'danger' as const },
          }
        : {}),
    },
    confirm: {
      testID: 'settings-confirm-sign-out',
      title: 'Sign out?',
      body: 'Your library stays on the server. Downloads on this device are removed.',
      confirmLabel: 'Sign out',
      onConfirm: () => void opts.signOut(),
    },
  };
}

export function buildDangerZoneActions(opts: {
  downloadCount: number;
  downloadBytes: number;
  downloadSize: string;
  signOutState: SignOutResult;
  clearHistory: ReturnType<typeof useClearSearchHistory>;
  lastUnpinAll?: UnpinAllOutcome | undefined;
  unpinAll: () => void;
  signOut: () => Promise<void>;
}): DangerZoneAction[] {
  return [
    removeDownloadsAction(opts),
    clearSearchHistoryAction(opts.clearHistory),
    signOutAction(opts),
  ];
}
