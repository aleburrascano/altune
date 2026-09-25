import type { TextTone } from '@shared/ui/primitives/Text';
import { failureCopyForAction } from '../failureCopyForAction';

type BackfillStatus = 'idle' | 'pending' | 'error' | 'success';

type ActionTone = Extract<TextTone, 'danger' | 'success' | 'accent'>;

type BackfillState = {
  status: BackfillStatus;
  error: unknown;
  data: { updated: number; scanned: number } | undefined;
};

const ACTION_LABELS: Record<BackfillStatus, string> = {
  idle: 'Run',
  pending: 'Running…',
  error: 'Retry',
  success: 'Done',
};

const ACTION_TONES: Record<BackfillStatus, ActionTone> = {
  idle: 'accent',
  pending: 'accent',
  error: 'danger',
  success: 'success',
};

export function backfillDetail(backfill: BackfillState): string | undefined {
  if (backfill.status === 'pending') return 'Resolving featured artists…';
  if (backfill.status === 'error') return failureCopyForAction(backfill.error);
  if (backfill.data == null) return undefined;
  return `Updated ${backfill.data.updated} of ${backfill.data.scanned} tracks`;
}

export function backfillActionLabel(backfill: BackfillState): string {
  return ACTION_LABELS[backfill.status];
}

export function backfillActionTone(backfill: BackfillState): ActionTone {
  return ACTION_TONES[backfill.status];
}
