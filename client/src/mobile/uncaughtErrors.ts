import ErrorUtils from 'react-native/Libraries/vendor/core/ErrorUtils';

type ErrorHandler = (error: unknown, isFatal?: boolean) => void;

export type GlobalErrorHandlers = {
  getGlobalHandler: () => ErrorHandler;
  setGlobalHandler: (onUncaught: ErrorHandler) => void;
};

export const noGlobalErrorHandlers: GlobalErrorHandlers = {
  getGlobalHandler: () => () => undefined,
  setGlobalHandler: () => undefined,
};

export function globalErrorHandlersFor(os: string): GlobalErrorHandlers {
  return os === 'web' ? noGlobalErrorHandlers : ErrorUtils;
}

export function chainUncaughtErrorHandler(
  onError: (error: unknown) => void,
  handlers: GlobalErrorHandlers,
): void {
  const previousHandler = handlers.getGlobalHandler();
  handlers.setGlobalHandler((error, isFatal) => {
    onError(error);
    previousHandler(error, isFatal);
  });
}
