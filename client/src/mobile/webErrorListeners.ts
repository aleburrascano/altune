import type { ChainUncaughtErrors, ChainUnhandledRejections } from '@shared/telemetry';

type WindowErrorEvent = { error?: unknown; message?: string };

type WindowRejectionEvent = { reason: unknown };

export type WindowEvents = {
  addEventListener(type: 'error', listener: (event: WindowErrorEvent) => void): void;
  addEventListener(
    type: 'unhandledrejection',
    listener: (event: WindowRejectionEvent) => void,
  ): void;
};

export function chainWindowUncaughtErrors(win: WindowEvents | undefined): ChainUncaughtErrors {
  return (onError) => {
    win?.addEventListener('error', (event) => {
      onError(event.error ?? new Error(event.message ?? 'window error'));
    });
  };
}

export function chainWindowUnhandledRejections(
  win: WindowEvents | undefined,
): ChainUnhandledRejections {
  return (onRejection) => {
    win?.addEventListener('unhandledrejection', (event) => onRejection(event.reason));
  };
}

export function runtimeWindow(): WindowEvents | undefined {
  const candidate = (globalThis as { window?: Partial<WindowEvents> }).window;
  return typeof candidate?.addEventListener === 'function'
    ? (candidate as WindowEvents)
    : undefined;
}
