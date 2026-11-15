import ErrorUtils from 'react-native/Libraries/vendor/core/ErrorUtils';

type ErrorHandler = (error: unknown, isFatal?: boolean) => void;

export type GlobalErrorHandlers = {
  getGlobalHandler: () => ErrorHandler;
  setGlobalHandler: (onUncaught: ErrorHandler) => void;
};

export function chainUncaughtErrorHandler(
  onError: (error: unknown) => void,
  handlers: GlobalErrorHandlers = ErrorUtils,
): void {
  const previousHandler = handlers.getGlobalHandler();
  handlers.setGlobalHandler((error, isFatal) => {
    onError(error);
    previousHandler(error, isFatal);
  });
}
