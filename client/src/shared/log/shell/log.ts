export interface Logger {
  warn(message: string, ...details: unknown[]): void;
}

type WarningSink = Pick<Console, 'warn'>;

export function consoleLogger(sink: WarningSink = console): Logger {
  return { warn: (message, ...details) => sink.warn(message, ...details) };
}

let current: Logger = consoleLogger();

export function setLogger(next: Logger = consoleLogger()): void {
  current = next;
}

export function warn(message: string, ...details: unknown[]): void {
  current.warn(message, ...details);
}
