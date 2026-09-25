import {
  ApiError,
  ContractError,
  NetworkError,
  isSessionFetchFailure,
} from '@shared/errors';
import { asyncView } from '@shared/lib/async-view';
import { RETRY_TAIL } from '@shared/lib/describeError';

export type ScreenView = 'loading' | 'error' | 'empty' | 'list';

/**
 * Why a library request failed, as far as a caller needs to react to it:
 * - `network`: never reached the API (offline, timeout, auth server unreachable); retry later.
 * - `auth`: the API refused the session (401/403); retrying without signing in cannot help.
 * - `not-found`: the resource is already gone (404/410); retrying cannot help.
 * - `server`: the API failed or answered with something unusable (5xx, 429, bad body); retry.
 * - `unknown`: anything else, handled like a generic failure.
 */
export type LibraryFailure = 'network' | 'auth' | 'not-found' | 'server' | 'unknown';

/**
 * The one place a thrown value from the library's API calls becomes a failure class.
 * Reads the typed errors `@shared/api-client` throws, never the message text.
 */
export function classifyLibraryError(error: unknown): LibraryFailure {
  if (error instanceof NetworkError || isSessionFetchFailure(error)) return 'network';
  if (error instanceof ContractError) return 'server';
  if (!(error instanceof ApiError)) return 'unknown';
  if (error.status === 401 || error.status === 403) return 'auth';
  if (error.status === 404 || error.status === 410) return 'not-found';
  if (error.status === 429 || error.status >= 500) return 'server';
  return 'unknown';
}

/** The closing ask of a failed-mutation Alert: a plain retry cannot fix a refused session. */
export function failureTail(failure: LibraryFailure): string {
  return failure === 'auth' ? 'Sign in again, then retry.' : RETRY_TAIL;
}

export type ScreenState = {
  view: ScreenView;
  /** Set only when the error is what the screen shows, so a stale error behind a spinner is not reported. */
  failure: LibraryFailure | null;
};

export function _viewForState(state: {
  isLoading: boolean;
  error: unknown;
  items: readonly unknown[];
}): ScreenState {
  const view = asyncView({
    isLoading: state.isLoading,
    isError: Boolean(state.error),
    isEmpty: state.items.length === 0,
  });
  if (view === 'error') return { view, failure: classifyLibraryError(state.error) };
  return { view: view === 'ready' ? 'list' : view, failure: null };
}
