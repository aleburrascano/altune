import { actionFailureDetail } from './actionFailureDetail';

type BackfillState = {
  isPending: boolean;
  isSuccess: boolean;
  isError: boolean;
  error: unknown;
  data: { updated: number; scanned: number } | undefined;
};

export function backfillDetail(backfill: BackfillState): string | undefined {
  if (backfill.isPending) return 'Resolving featured artists…';
  if (backfill.isError) return actionFailureDetail(backfill.error);
  if (backfill.data == null) return undefined;
  return `Updated ${backfill.data.updated} of ${backfill.data.scanned} tracks`;
}

export function backfillActionLabel(backfill: BackfillState): string {
  if (backfill.isPending) return 'Running…';
  if (backfill.isError) return 'Retry';
  return backfill.isSuccess ? 'Done' : 'Run';
}

export function backfillActionTone(backfill: BackfillState): 'danger' | 'success' | 'accent' {
  if (backfill.isError) return 'danger';
  return backfill.isSuccess ? 'success' : 'accent';
}
