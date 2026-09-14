import { ApiError } from '@shared/api-client';

export function submitFailureMessage(error: unknown): string {
  if (error instanceof ApiError && error.status === 400) {
    return 'That report was rejected — try describing it in a bit more detail.';
  }
  return 'Could not reach the server. Your report is saved — try again when you have signal.';
}
