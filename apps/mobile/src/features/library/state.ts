import { ApiError, ContractError, NetworkError, isSessionFetchFailure } from '@shared/errors';
import { asyncView } from '@shared/lib/async-view';
import { RETRY_TAIL } from '@shared/lib/describeError';

type ScreenView = 'loading' | 'error' | 'empty' | 'list';

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

type ScreenState = {
  view: ScreenView;
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
