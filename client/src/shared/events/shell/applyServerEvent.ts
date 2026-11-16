import type { EventCache } from '@shared/events/core/eventCache';
import {
  type ServerEventHandlers,
  isServerEventType,
  type ServerEventType,
} from '@shared/events/core/serverEventTypes';
import type { ServerEvent } from '@shared/events/core/sseFrames';
import { ACQUISITION_HANDLERS } from '@shared/events/shell/acquisitionEvents';
import { PLAYLIST_HANDLERS } from '@shared/events/shell/playlistEvents';
import { RESYNC_HANDLERS } from '@shared/events/shell/resyncEvents';

const HANDLERS: ServerEventHandlers<ServerEventType> = {
  ...RESYNC_HANDLERS,
  ...ACQUISITION_HANDLERS,
  ...PLAYLIST_HANDLERS,
};

export function applyServerEvent(cache: EventCache, event: ServerEvent): boolean {
  if (!isServerEventType(event.type)) return false;
  HANDLERS[event.type](cache, event);
  return true;
}
