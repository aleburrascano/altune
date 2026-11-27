import type { EventCache } from '@shared/events/core/eventCache';
import { EVENT_INVALIDATES } from '@shared/events/core/eventInvalidation';
import type { ServerEventHandler, ServerEventType } from '@shared/events/core/serverEventTypes';

export function invalidateForEvent(cache: EventCache, type: ServerEventType): void {
  for (const queryKey of EVENT_INVALIDATES[type]) {
    void cache.invalidateQueries({ queryKey });
  }
}

export function invalidatesOn(type: ServerEventType): ServerEventHandler {
  return (cache) => invalidateForEvent(cache, type);
}
