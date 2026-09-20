import { ApiError, correlationIdOf } from '@shared/errors';

import { classifyLibraryError, type LibraryFailure } from './state';

export type FailureLogFields = {
  status?: number;
  code?: string;
  failure: LibraryFailure;
  correlationId?: string;
};

/**
 * The most a library failure may carry into a log, so redaction lives here rather
 * than at each call site (#1703). Never the caught error itself: its message can
 * hold a server message or a search term, its stack the local paths. The
 * correlation id matches the server's log lines, so triage reads the message there.
 */
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
