import { ApiError, correlationIdOf } from '@shared/errors';

import { classifyLibraryError, type LibraryFailure } from './state';

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
