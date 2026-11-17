import type { FailureAlert } from '@shared/ui';
import type { LibraryPort } from '@features/library';

type ConfirmRequest = Parameters<LibraryPort['confirmDestructive']>[0];
type TitleAndMessage = [string, string];

export interface RecordingDialogs {
  alerts: FailureAlert[];
  prompts: ConfirmRequest[];
  showFailure: LibraryPort['showFailure'];
  confirmDestructive: LibraryPort['confirmDestructive'];
  shown: () => TitleAndMessage[];
  failNextAlert: (error: Error) => void;
}

export function recordingDialogs(): RecordingDialogs {
  const alerts: FailureAlert[] = [];
  const prompts: ConfirmRequest[] = [];
  let nextAlertError: Error | undefined;
  return {
    alerts,
    prompts,
    showFailure(alert) {
      alerts.push(alert);
      const error = nextAlertError;
      nextAlertError = undefined;
      if (error) throw error;
    },
    confirmDestructive(request) {
      prompts.push(request);
    },
    shown: () => alerts.map(({ title, message }) => [title, message]),
    failNextAlert(error) {
      nextAlertError = error;
    },
  };
}
