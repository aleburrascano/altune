import type { ServerEventHandlers } from '@shared/events/core/serverEventTypes';
import { invalidatesOn } from '@shared/events/shell/invalidateOnEvent';

export const RESYNC_HANDLERS: ServerEventHandlers<'resync'> = {
  resync: invalidatesOn('resync'),
};
