import type { QueryClient } from '@tanstack/react-query';

import { ACQUISITION_HANDLERS } from '@shared/events/acquisitionEvents';
import type { ServerEventHandlers } from '@shared/events/eventPayload';
import {
  isServerEventType,
  recordUnhandledEvent,
  type ServerEventType,
} from '@shared/events/eventTypes';
import { PLAYLIST_HANDLERS } from '@shared/events/playlistEvents';
import { RESYNC_HANDLERS } from '@shared/events/resyncEvents';
import type { ServerEvent } from '@shared/events/sse-client';

const HANDLERS: ServerEventHandlers<ServerEventType> = {
  ...RESYNC_HANDLERS,
  ...ACQUISITION_HANDLERS,
  ...PLAYLIST_HANDLERS,
};

export function applyServerEvent(queryClient: QueryClient, event: ServerEvent): void {
  if (!isServerEventType(event.type)) {
    recordUnhandledEvent(event.type);
    return;
  }
  HANDLERS[event.type](queryClient, event);
}
