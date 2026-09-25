import { ApiError, NetworkError } from '@shared/api-client';

// Row detail copy for a failed settings action (backfill, clear history).
export function failureCopyForAction(error: unknown): string {
  if (error instanceof NetworkError) {
    return 'Could not reach the server — check your connection and try again.';
  }
  if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
    return 'Your session has expired — sign in again and retry.';
  }
  if (error instanceof ApiError && error.status >= 500) {
    return 'The server had a problem — try again in a few minutes.';
  }
  return 'Something went wrong — try again.';
}
