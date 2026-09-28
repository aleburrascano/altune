import type { QueryClient } from '@tanstack/react-query';

import { ACQUISITION_HANDLERS } from './acquisitionEvents';
import type { ServerEventHandlers } from './eventPayload';
import { isServerEventType, recordUnhandledEvent, type ServerEventType } from './eventTypes';
import { PLAYLIST_HANDLERS } from './playlistEvents';
import { RESYNC_HANDLERS } from './resyncEvents';
import type { ServerEvent } from './sse-client';

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
