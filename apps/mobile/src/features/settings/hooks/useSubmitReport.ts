import { useMutation } from '@tanstack/react-query';

import { submitReport } from '@shared/api-client/feedback';
import type { SubmitReportInput } from '@shared/api-client/feedback';

export type SubmitReportVariables = SubmitReportInput & { idempotencyKey: string };

export function useSubmitReport() {
  return useMutation({
    mutationFn: ({ idempotencyKey, ...report }: SubmitReportVariables) =>
      submitReport(report, idempotencyKey),
    retry: false,
    onError: (error) => {
      console.warn('[feedback] report submission failed', error);
    },
  });
}
