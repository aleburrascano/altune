import type { QueryClient } from '@tanstack/react-query';

import type { ServerEvent } from './sse-client';
import type { ServerEventType } from './eventTypes';

export type ServerEventHandler = (queryClient: QueryClient, event: ServerEvent) => void;

// A domain module's slice of the router table: exactly the event types it owns.
export type ServerEventHandlers<K extends ServerEventType> = Record<K, ServerEventHandler>;

export function asString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

export function stringArray(value: unknown): string[] | null {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : null;
}
