export type LogLevel = 'warn' | 'error';

export interface LogEntry {
  readonly level: LogLevel;
  readonly tag: string | undefined;
  readonly message: string;
  readonly details: readonly unknown[];
}

export interface Logger {
  warn(message: string, ...details: unknown[]): void;
  error(message: string, ...details: unknown[]): void;
}

type ConsoleSink = Pick<Console, 'warn' | 'error'>;

const TAG_PREFIX_PATTERN = /^\[([^\]]+)\]\s*(.*)$/s;

export function entryOf(level: LogLevel, message: string, details: readonly unknown[]): LogEntry {
  const match = TAG_PREFIX_PATTERN.exec(message);
  return { level, tag: match?.[1], message: match?.[2] ?? message, details };
}

function renderedMessage({ tag, message }: LogEntry): string {
  return tag === undefined ? message : `[${tag}] ${message}`;
}

export function consoleLogger(sink: ConsoleSink = console): Logger {
  return {
    warn: (message, ...details) => {
      const entry = entryOf('warn', message, details);
      sink.warn(renderedMessage(entry), ...entry.details);
    },
    error: (message, ...details) => {
      const entry = entryOf('error', message, details);
      sink.error(renderedMessage(entry), ...entry.details);
    },
  };
}

let current: Logger = consoleLogger();

export function setLogger(next: Logger = consoleLogger()): void {
  current = next;
}

export function warn(message: string, ...details: unknown[]): void {
  current.warn(message, ...details);
}

export function error(message: string, ...details: unknown[]): void {
  current.error(message, ...details);
}
