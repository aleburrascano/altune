import {
  ApiError,
  ContractError,
  NetworkError,
  isSessionFetchFailure,
  correlationIdOf,
} from '@shared/errors';
import { RETRY_TAIL } from '@shared/failure/describeError';
import type { FailureAlert } from '@shared/ui';
import type { TrackId } from '@shared/api-client/ids';

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

type FailureLogFields = {
  status?: number;
  code?: string;
  failure: LibraryFailure;
  correlationId?: string;
};

export function failureLogFields(error: unknown): FailureLogFields {
  const correlationId = correlationIdOf(error);
  return {
    ...(error instanceof ApiError
      ? { status: error.status, ...(error.code === undefined ? {} : { code: error.code }) }
      : {}),
    failure: classifyLibraryError(error),
    ...(correlationId === undefined ? {} : { correlationId }),
  };
}

export function libraryFailureAlert(
  surface: string,
  title: string,
  lead: string,
  failure: LibraryFailure,
): FailureAlert {
  return { surface, title, message: `${lead} ${failureTail(failure)}` };
}

export function logTrackMutationFailure(
  action: string,
  endpoint: (trackId: TrackId) => string,
  trackId: TrackId,
  error: unknown,
): void {
  console.warn(`[library] ${action} failed`, {
    trackId,
    endpoint: endpoint(trackId).split('?')[0],
    ...failureLogFields(error),
  });
}

export function trackNotFoundAlert(trackId: TrackId): FailureAlert {
  return {
    surface: 'library.track_not_found',
    title: 'Track not found',
    message: 'This track is no longer in your library.',
    trackId,
  };
}
