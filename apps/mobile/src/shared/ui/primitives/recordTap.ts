import { recordUserAction } from '../../telemetry/userTelemetry';

export function recordTap(action: string | undefined, trackId: string | undefined): void {
  if (action === undefined) {
    return;
  }
  recordUserAction({
    action,
    outcome: 'tapped',
    ...(trackId === undefined ? {} : { track_id: trackId }),
  });
}
