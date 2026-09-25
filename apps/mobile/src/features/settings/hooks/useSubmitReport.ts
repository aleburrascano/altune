import { useMutation } from '@tanstack/react-query';

import { submitReport } from '@shared/api-client/feedback';
import type { SubmitReportInput } from '@shared/api-client/feedback';

/** `idempotencyKey` is the draft's own key, not a wire field: it travels as a header. */
export type SubmitReportVariables = SubmitReportInput & { idempotencyKey: string };

export function useSubmitReport() {
  return useMutation({
    mutationFn: ({ idempotencyKey, ...report }: SubmitReportVariables) =>
      submitReport(report, idempotencyKey),
    // Intentional: the draft's key lets the server collapse a repeat, but its
    // dedup store is in-memory and per-instance, so an automatic retry that
    // lands on a restarted or a different instance still files a duplicate
    // GitHub issue. This mutation never adopts the transient-retry policy
    // backfill/clear-history use.
    retry: false,
    onError: (error) => {
      console.warn('[feedback] report submission failed', error);
    },
  });
}
