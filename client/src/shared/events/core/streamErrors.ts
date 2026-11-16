export class ServerEventHandlerError extends Error {
  readonly eventId: string;
  readonly eventType: string;

  constructor(eventId: string, eventType: string, cause: unknown) {
    super(`SSE event handler threw (type=${eventType}, id=${eventId || '<none>'})`, { cause });
    this.name = 'ServerEventHandlerError';
    this.eventId = eventId;
    this.eventType = eventType;
  }
}

export class SSEConnectionError extends Error {
  readonly correlationId: string | null;

  constructor(correlationId: string | null) {
    super(
      correlationId === null
        ? 'SSE connection error'
        : `SSE connection error (correlation_id=${correlationId})`,
    );
    this.name = 'SSEConnectionError';
    this.correlationId = correlationId;
  }
}

export class SSEHttpError extends Error {
  readonly status: number;
  readonly correlationId: string | null;

  constructor(status: number, correlationId: string | null) {
    super(
      correlationId === null
        ? `SSE stream refused (status=${status})`
        : `SSE stream refused (status=${status}, correlation_id=${correlationId})`,
    );
    this.name = 'SSEHttpError';
    this.status = status;
    this.correlationId = correlationId;
  }
}
