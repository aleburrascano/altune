import type { UserActionPayload } from '@shared/telemetry';

export function tapAction(action: string, trackId: string | undefined): UserActionPayload {
  return {
    action,
    outcome: 'tapped',
    ...(trackId === undefined ? {} : { track_id: trackId }),
  };
}
