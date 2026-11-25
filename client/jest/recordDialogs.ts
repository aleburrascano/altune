import { installDialogs, platformDialogs, type ConfirmOptions } from '@shared/ui';

type TitleAndMessage = [string, string];

export interface RecordedDialogs {
  alerts: TitleAndMessage[];
  confirms: ConfirmOptions[];
  restore: () => void;
}

export function recordDialogs(): RecordedDialogs {
  const alerts: TitleAndMessage[] = [];
  const confirms: ConfirmOptions[] = [];
  installDialogs({
    alert: (title, message) => {
      alerts.push([title, message]);
    },
    confirmDestructive: (options) => {
      confirms.push(options);
    },
  });
  return { alerts, confirms, restore: () => installDialogs(platformDialogs) };
}
