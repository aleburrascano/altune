type BackfillState = {
  isPending: boolean;
  isSuccess: boolean;
  data: { updated: number; scanned: number } | undefined;
};

export function backfillDetail(backfill: BackfillState): string | undefined {
  if (backfill.isPending) return 'Resolving featured artists…';
  if (backfill.data == null) return undefined;
  return `Updated ${backfill.data.updated} of ${backfill.data.scanned} tracks`;
}

export function backfillActionLabel(backfill: BackfillState): string {
  if (backfill.isPending) return 'Running…';
  return backfill.isSuccess ? 'Done' : 'Run';
}
