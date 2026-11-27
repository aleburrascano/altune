import {
  ApiError,
  ContractError,
  NetworkError,
  failureKindOf,
  isSessionFetchFailure,
  correlationIdOf,
  RETRY_TAIL,
  type ApiErrorCode,
} from '@shared/failure';
import type { FailureAlert } from '@shared/ui';
import type { TrackId } from '@shared/catalog';

export type LibraryWarn = (message: string, fields: Record<string, unknown>) => void;

export type ShowFailure = (alert: FailureAlert) => void;

export type LibraryFailure = 'network' | 'auth' | 'not-found' | 'server' | 'unknown';

export function classifyLibraryError(error: unknown): LibraryFailure {
  if (error instanceof NetworkError || isSessionFetchFailure(error)) return 'network';
  if (error instanceof ContractError) return 'server';
  if (!(error instanceof ApiError)) return 'unknown';
  const kind = failureKindOf(error);
  if (kind === 'unauthorized') return 'auth';
  if (kind === 'not-found') return 'not-found';
  if (kind === 'rate-limited' || kind === 'server' || kind === 'timeout') return 'server';
  return 'unknown';
}

export function failureTail(failure: LibraryFailure): string {
  return failure === 'auth' ? 'Sign in again, then retry.' : RETRY_TAIL;
}

type FailureLogFields = {
  status?: number;
  code?: string;
  failure: LibraryFailure;
  corr_id?: string;
};

const AUDIO_ORPHANED_CODE: ApiErrorCode = 'catalog.audio_orphaned';

export function isTrackGoneAfterDelete(error: unknown): boolean {
  if (error instanceof ApiError && error.code === AUDIO_ORPHANED_CODE) return true;
  return classifyLibraryError(error) === 'not-found';
}

export function failureLogFields(error: unknown): FailureLogFields {
  const correlationId = correlationIdOf(error);
  const fields: FailureLogFields = { failure: classifyLibraryError(error) };
  if (error instanceof ApiError) {
    fields.status = error.status;
    if (error.code !== undefined) fields.code = error.code;
  }
  if (correlationId !== undefined) fields.corr_id = correlationId;
  return fields;
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
  warn: LibraryWarn,
  action: string,
  endpoint: (trackId: TrackId) => string,
  trackId: TrackId,
  error: unknown,
): void {
  warn(`[library] ${action} failed`, {
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
