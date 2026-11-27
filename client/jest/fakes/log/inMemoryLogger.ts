import { setLogger } from '@shared/log';

export type LoggedLine = [message: string, ...details: unknown[]];

export interface RecordedLogs {
  readonly warnings: LoggedLine[];
  readonly errors: LoggedLine[];
  text(): string;
}

function revealError(_key: string, value: unknown): unknown {
  return value instanceof Error ? `${value.name}: ${value.message} ${value.stack ?? ''}` : value;
}

export function recordLogs(): RecordedLogs {
  const warnings: LoggedLine[] = [];
  const errors: LoggedLine[] = [];
  setLogger({
    warn: (message, ...details) => {
      warnings.push([message, ...details]);
    },
    error: (message, ...details) => {
      errors.push([message, ...details]);
    },
  });
  return { warnings, errors, text: () => JSON.stringify([...warnings, ...errors], revealError) };
}

export function captureWarnings(): jest.Mock {
  const warn = jest.fn();
  setLogger({ warn, error: jest.fn() });
  return warn;
}
