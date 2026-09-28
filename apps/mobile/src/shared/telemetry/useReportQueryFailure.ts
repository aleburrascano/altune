import { useEffect, useRef } from 'react';
import { isCancelledError } from '@tanstack/react-query';

import { ApiError, correlationIdOf } from '@shared/errors';

import { useRecordEvent } from './useRecordEvent';

export type QueryFailureSource = 'search' | 'suggest' | 'history' | 'clear_history';

const reported = new WeakSet<Error>();

export function useReportQueryFailure(error: Error | null, source: QueryFailureSource): void {
  const recordEvent = useRecordEvent();
  const recordRef = useRef(recordEvent);

  useEffect(() => {
    recordRef.current = recordEvent;
  });

  useEffect(() => {
    if (error === null || isCancelledError(error) || reported.has(error)) return;
    reported.add(error);
    const payload: Record<string, unknown> = { source };
    if (error instanceof ApiError) payload['status'] = error.status;
    const correlationId = correlationIdOf(error);
    if (correlationId !== undefined) payload['correlationId'] = correlationId;
    recordRef.current.mutate({ type: 'search_failed', payload });
  }, [error, source]);
}
