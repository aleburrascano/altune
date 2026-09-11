import { isNetworkError } from './isNetworkError';

export interface ErrorCopy {
  readonly title: string;
  readonly body: string;
}

/**
 * The shared "…try again" tail. Rendered error bodies and the mutation Alerts
 * compose their own verb sentence in front of it so the retry ask reads the
 * same everywhere.
 */
export const RETRY_TAIL = 'Please try again.';

/**
 * A 5xx from the API. Detected structurally by the `status` field `ApiError`
 * carries (see `@shared/api-client/errors`): this module lives in `shared/lib`,
 * whose purity invariant forbids a runtime import of `@shared/api-client`, so it
 * reads the same contract without pulling the class in.
 */
function isServerError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  const status = (err as Error & { status?: unknown }).status;
  return typeof status === 'number' && status >= 500;
}

/**
 * The one mapper from an unknown thrown value to user-facing `{ title, body }`.
 * Distinguishes offline transport failure from a server-side 5xx from an
 * otherwise-generic error, so a screen never hand-writes the network-vs-server
 * branch again.
 */
export function describeError(err: unknown): ErrorCopy {
  if (isNetworkError(err)) {
    return { title: 'No connection', body: 'Check your connection and try again.' };
  }
  if (isServerError(err)) {
    return { title: 'Something went wrong', body: `Something went wrong on our end. ${RETRY_TAIL}` };
  }
  return { title: 'Something went wrong', body: `Something went wrong. ${RETRY_TAIL}` };
}
