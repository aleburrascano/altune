import { ApiError, NetworkError } from '@shared/api-client';

// The draft lives only in the modal's in-memory state, so no copy here may
// claim it is saved.
export function failureCopyForReport(error: unknown): string {
  if (error instanceof NetworkError) {
    return 'Could not reach the server — check your connection and try again.';
  }
  if (!(error instanceof ApiError)) {
    return 'Something went wrong sending your report — try again.';
  }
  if (error.status === 400) {
    return 'That report was rejected — try describing it in a bit more detail.';
  }
  if (error.status === 401 || error.status === 403) {
    return 'You need to be signed in to send a report — sign in again and retry.';
  }
  if (error.status === 429) {
    return 'You have sent a lot of reports recently — try again in a little while.';
  }
  if (error.status >= 500) {
    return 'The server had a problem filing your report — try again in a few minutes.';
  }
  return `The server could not accept this report (error ${error.status}). Try again later.`;
}
