import { ApiError, ContractError, NetworkError, isSessionFetchFailure } from '@shared/errors';
import { asyncView, type AsyncView } from '@shared/lib/async-view';
import { RETRY_TAIL } from '@shared/lib/describeError';

export type LibraryFailure = 'network' | 'auth' | 'not-found' | 'server' | 'unknown';

export function classifyLibraryError(error: unknown): LibraryFailure {
  if (error instanceof NetworkError || isSessionFetchFailure(error)) return 'network';
  if (error instanceof ContractError) return 'server';
  if (!(error instanceof ApiError)) return 'unknown';
  if (error.status === 401 || error.status === 403) return 'auth';
  if (error.status === 404 || error.status === 410) return 'not-found';
  if (error.status === 429 || error.status >= 500) return 'server';
  return 'unknown';
}

export function failureTail(failure: LibraryFailure): string {
  return failure === 'auth' ? 'Sign in again, then retry.' : RETRY_TAIL;
}

type LibrarySectionInput = {
  isLoading: boolean;
  error: unknown;
  count: number;
  showEmpty: boolean;
};

export function librarySection(input: LibrarySectionInput): AsyncView {
  const { isLoading, error, count, showEmpty } = input;
  return asyncView({ isLoading, isError: Boolean(error), isEmpty: count === 0 && showEmpty });
}
