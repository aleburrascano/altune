import { useMutation } from '@tanstack/react-query';

import { ApiError } from '@shared/api-client';
import { submitReport } from '@shared/api-client/feedback';
import type { SubmitReportInput } from '@shared/api-client/feedback';

export function useSubmitReport() {
  return useMutation({
    mutationFn: (input: SubmitReportInput) => submitReport(input),
    retry: false,
  });
}

export function submitFailureMessage(error: unknown): string {
  if (error instanceof ApiError && error.status === 400) {
    return 'That report was rejected — try describing it in a bit more detail.';
  }
  return 'Could not reach the server. Your report is saved — try again when you have signal.';
}
