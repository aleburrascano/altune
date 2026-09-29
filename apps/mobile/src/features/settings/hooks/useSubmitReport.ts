import { submitReport } from '@shared/api-client/feedback';
import type { SubmitReportInput } from '@shared/api-client/feedback';
import { useAppMutation } from '@shared/query/useAppMutation';

export type SubmitReportVariables = SubmitReportInput & { idempotencyKey: string };

export function useSubmitReport() {
  return useAppMutation({
    action: 'settings.submit_report',
    mutationFn: ({ idempotencyKey, ...report }: SubmitReportVariables) =>
      submitReport(report, idempotencyKey),
    retry: false,
    onError: (error) => {
      console.warn('[feedback] report submission failed', error);
    },
  });
}
