import { Eraser, LogOut, Trash2, type LucideIcon } from 'lucide-react-native';

import type { SignOutResult } from '@shared/auth/useSignOut';
import type { TextTone } from '@shared/ui/primitives/Text';
import { tracksLabel, LEFTOVER_FILES_LABEL } from '../downloadStatsModel';
import { failureCopyForAction } from '../failureCopyForAction';
import type { RemoveDownloads } from '../hooks/useRemoveDownloads';

// Closed on purpose: the open confirm is chosen by comparing against this key,
// so a value outside the set would match no confirm and open nothing.
export type DangerZoneActionKey = 'downloads' | 'history' | 'sign-out';

export type ClearHistoryState = {
  isPending: boolean;
  isError: boolean;
  isSuccess: boolean;
  error: unknown;
  mutate: () => void;
};

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
    return `${LEFTOVER_FILES_LABEL} (${downloadSize}) will be deleted from this device.`;
  }
  return `${tracksLabel(downloadCount)} (${downloadSize}) will be deleted from this device. They stay in your library and can be downloaded again.`;
}

function failedOutcome(detail: string): Pick<DangerZoneAction['row'], 'detail' | 'status'> {
  return { detail, status: { label: 'Failed', tone: 'danger' } };
}

// A remove-all that cleared everything hides the row, so only the partial pass has a state to show.
function removeDownloadsOutcome(
  lastUnpinAll: RemoveDownloads['lastUnpinAll'],
): Pick<DangerZoneAction['row'], 'detail' | 'status'> {
  if (lastUnpinAll !== 'partial') return {};
  return failedOutcome("Some downloads couldn't be removed — try again.");
}

function removeDownloadsRow(downloads: RemoveDownloads): DangerZoneAction['row'] {
  const { stats, lastUnpinAll } = downloads;
  return {
    testID: 'settings-remove-downloads',
    label: 'Remove all downloads',
    detail: `Frees ${stats.downloadSize} · tracks stay in your library`,
    hidden: stats.usage === 'none',
    ...removeDownloadsOutcome(lastUnpinAll),
  };
}

function removeDownloadsConfirm(downloads: RemoveDownloads): DangerZoneAction['confirm'] {
  const { stats, unpinAll } = downloads;
  return {
    testID: 'settings-confirm-remove-downloads',
    title: 'Remove all downloads?',
    body: removeDownloadsBody(stats.downloadCount, stats.downloadSize),
    confirmLabel: 'Remove',
    onConfirm: unpinAll,
  };
}

function removeDownloadsAction(downloads: RemoveDownloads): DangerZoneAction {
  return {
    key: 'downloads',
    icon: Trash2,
    row: removeDownloadsRow(downloads),
    confirm: removeDownloadsConfirm(downloads),
  };
}

function clearHistoryOutcome(
  clearHistory: ClearHistoryState,
): Pick<DangerZoneAction['row'], 'detail' | 'status'> {
  if (clearHistory.isError) return failedOutcome(failureCopyForAction(clearHistory.error));
  return clearHistory.isSuccess ? { status: { label: 'Cleared', tone: 'success' } } : {};
}

function clearHistoryRow(clearHistory: ClearHistoryState): DangerZoneAction['row'] {
  return {
    testID: 'settings-clear-search-history',
    label: 'Clear search history',
    disabled: clearHistory.isPending,
    ...clearHistoryOutcome(clearHistory),
  };
}

function clearHistoryConfirm(clearHistory: ClearHistoryState): DangerZoneAction['confirm'] {
  return {
    testID: 'settings-confirm-clear-history',
    title: 'Clear search history?',
    body: 'Your recent searches will be deleted from this device and the server.',
    confirmLabel: 'Clear',
    onConfirm: () => clearHistory.mutate(),
  };
}

function clearSearchHistoryAction(clearHistory: ClearHistoryState): DangerZoneAction {
  return {
    key: 'history',
    icon: Eraser,
    row: clearHistoryRow(clearHistory),
    confirm: clearHistoryConfirm(clearHistory),
  };
}

function signOutOutcome(signOutState: SignOutResult): Pick<DangerZoneAction['row'], 'detail' | 'status'> {
  return signOutState.status === 'error'
    ? failedOutcome(failureCopyForAction(signOutState.error))
    : {};
}

function signOutRow(signOutState: SignOutResult): DangerZoneAction['row'] {
  return {
    testID: 'settings-sign-out',
    label: 'Sign out',
    disabled: signOutState.status === 'loading',
    ...signOutOutcome(signOutState),
  };
}

function signOutConfirm(signOut: () => Promise<void>): DangerZoneAction['confirm'] {
  return {
    testID: 'settings-confirm-sign-out',
    title: 'Sign out?',
    body: 'Your library stays on the server. Downloads on this device are removed.',
    confirmLabel: 'Sign out',
    onConfirm: () => void signOut(),
  };
}

function signOutAction(opts: {
  signOutState: SignOutResult;
  signOut: () => Promise<void>;
}): DangerZoneAction {
  return {
    key: 'sign-out',
    icon: LogOut,
    row: signOutRow(opts.signOutState),
    confirm: signOutConfirm(opts.signOut),
  };
}

export function buildDangerZoneActions(opts: {
  downloads: RemoveDownloads;
  clearHistory: ClearHistoryState;
  signOutState: SignOutResult;
  signOut: () => Promise<void>;
}): DangerZoneAction[] {
  return [
    removeDownloadsAction(opts.downloads),
    clearSearchHistoryAction(opts.clearHistory),
    signOutAction(opts),
  ];
}
