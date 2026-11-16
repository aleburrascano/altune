const { __http } = require('@testing/doubles/fetch.js');

const EVENTS_PATH = '/v1/discovery/events';
const NO_CONTENT = 204;

type LoggedRequest = { method: string; path: string; body: string };
type RecordedEvent = { type: string; payload: { surface?: string } };

export function acceptTelemetry(): void {
  __http.reply(`POST ${EVENTS_PATH}`, { status: NO_CONTENT });
}

function isTelemetryPost(request: LoggedRequest): boolean {
  return request.method === 'POST' && request.path === EVENTS_PATH;
}

export function shownFailureSurfaces(): string[] {
  return (__http.requests as LoggedRequest[])
    .filter(isTelemetryPost)
    .map((request) => JSON.parse(request.body) as RecordedEvent)
    .filter((event) => event.type === 'failure_shown')
    .map((event) => String(event.payload.surface));
}
