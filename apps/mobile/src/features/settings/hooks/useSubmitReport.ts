import { useMutation } from '@tanstack/react-query';

import { submitReport } from '@shared/api-client/feedback';
import type { SubmitReportInput } from '@shared/api-client/feedback';

export function useSubmitReport() {
  return useMutation({
    mutationFn: (input: SubmitReportInput) => submitReport(input),
    // Intentional: a retried submit can file a duplicate GitHub issue, so this
    // mutation never adopts the transient-retry policy backfill/clear-history use.
    retry: false,
    onError: (error) => {
      console.warn('[feedback] report submission failed', error);
    },
  });
}
