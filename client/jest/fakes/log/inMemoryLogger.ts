import { setLogger } from '@shared/log';

export type LoggedLine = [message: string, ...details: unknown[]];

export interface RecordedLogs {
  readonly warnings: LoggedLine[];
  text(): string;
}

function revealError(_key: string, value: unknown): unknown {
  return value instanceof Error ? `${value.name}: ${value.message} ${value.stack ?? ''}` : value;
}

export function recordLogs(): RecordedLogs {
  const warnings: LoggedLine[] = [];
  setLogger({
    warn: (message, ...details) => {
      warnings.push([message, ...details]);
    },
  });
  return { warnings, text: () => JSON.stringify(warnings, revealError) };
}
