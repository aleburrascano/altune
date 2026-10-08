import { recordUserAction } from '@shared/telemetry/userTelemetry';

export function recordTap(action: string, trackId: string | undefined): void {
  recordUserAction({
    action,
    outcome: 'tapped',
    ...(trackId === undefined ? {} : { track_id: trackId }),
  });
}
