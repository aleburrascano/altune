import { recordFailureShown } from '@shared/telemetry/userTelemetry';
import { showAlert } from '@shared/ui/dialog/dialog';

const MAX_MESSAGE_LENGTH = 200;

export type FailureAlert = { surface: string; title: string; message: string; trackId?: string };

export function showFailureAlert({ surface, title, message, trackId }: FailureAlert): void {
  recordFailureShown({
    surface,
    message: message.slice(0, MAX_MESSAGE_LENGTH),
    ...(trackId === undefined ? {} : { track_id: trackId }),
  });
  showAlert(title, message);
}
