import type { QueryClient } from '@tanstack/react-query';

import { ACQUISITION_HANDLERS } from '@shared/events/acquisitionEvents';
import {
  type ServerEventHandlers,
  isServerEventType,
  recordUnhandledEvent,
  type ServerEventType,
} from '@shared/events/serverEventTypes';
import { PLAYLIST_HANDLERS } from '@shared/events/playlistEvents';
import { RESYNC_HANDLERS } from '@shared/events/resyncEvents';
import type { ServerEvent } from '@shared/events/sseFrames';

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
