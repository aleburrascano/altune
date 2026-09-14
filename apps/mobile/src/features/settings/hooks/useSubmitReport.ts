import { useMutation } from '@tanstack/react-query';

import { submitReport } from '@shared/api-client/feedback';
import type { SubmitReportInput } from '@shared/api-client/feedback';

export function useSubmitReport() {
  return useMutation({
    mutationFn: (input: SubmitReportInput) => submitReport(input),
    retry: false,
  });
}
