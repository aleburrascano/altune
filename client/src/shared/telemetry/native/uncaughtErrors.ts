import ErrorUtils from 'react-native/Libraries/vendor/core/ErrorUtils';

export function chainUncaughtErrorHandler(onError: (error: unknown) => void): void {
  const previousHandler = ErrorUtils.getGlobalHandler();
  ErrorUtils.setGlobalHandler((error, isFatal) => {
    onError(error);
    previousHandler(error, isFatal);
  });
}
