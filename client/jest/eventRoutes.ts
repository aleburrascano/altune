export const EVENT_ROUTES = ['POST /v1/discovery/events', 'POST /v1/telemetry/events'];

const EVENT_PATHS: readonly string[] = ['/v1/discovery/events', '/v1/telemetry/events'];

export function isEventPath(path: string): boolean {
  return EVENT_PATHS.includes(path);
}
