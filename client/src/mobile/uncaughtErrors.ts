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

export function handlersOrNone(runtime: GlobalErrorHandlers | undefined): GlobalErrorHandlers {
  return runtime ?? noGlobalErrorHandlers;
}

export function runtimeErrorHandlers(): GlobalErrorHandlers {
  return handlersOrNone(ErrorUtils);
}

export function chainUncaughtErrorHandler(
  handlers: GlobalErrorHandlers,
): (onError: (error: unknown) => void) => void {
  return (onError) => {
    const previousHandler = handlers.getGlobalHandler();
    handlers.setGlobalHandler((error, isFatal) => {
      onError(error);
      previousHandler(error, isFatal);
    });
  };
}
