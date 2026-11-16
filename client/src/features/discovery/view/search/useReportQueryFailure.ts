import { useEffect, useRef } from 'react';
import { isCancelledError } from '@tanstack/react-query';

import { ApiError, correlationIdOf } from '@shared/failure';

import { useRecordEvent } from '@shared/telemetry';

export type QueryFailureSource =
  'search' | 'unreleased_search' | 'suggest' | 'history' | 'record_history' | 'clear_history';

type SearchFailedPayload = {
  source: QueryFailureSource;
  status?: number;
  correlationId?: string;
};

const reported = new WeakSet<Error>();

function searchFailedPayload(error: Error, source: QueryFailureSource): SearchFailedPayload {
  const payload: SearchFailedPayload = { source };
  if (error instanceof ApiError) payload.status = error.status;
  const correlationId = correlationIdOf(error);
  if (correlationId !== undefined) payload.correlationId = correlationId;
  return payload;
}

export function useReportQueryFailure(error: Error | null, source: QueryFailureSource): void {
  const recordEvent = useRecordEvent();
  const recordRef = useRef(recordEvent);

  useEffect(() => {
    recordRef.current = recordEvent;
  });

  useEffect(() => {
    if (error === null || isCancelledError(error) || reported.has(error)) return;
    reported.add(error);
    recordRef.current.mutate({
      type: 'search_failed',
      payload: searchFailedPayload(error, source),
    });
  }, [error, source]);
}
