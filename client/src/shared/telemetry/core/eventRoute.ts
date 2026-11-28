import { isOperationalEvent, type ClientEvent } from '@shared/telemetry/core/operationalEvent';

const DISCOVERY_PATH = '/v1/discovery/events';
const TELEMETRY_PATH = '/v1/telemetry/events';

export function eventPath(event: ClientEvent): string {
  return isOperationalEvent(event) ? TELEMETRY_PATH : DISCOVERY_PATH;
}
