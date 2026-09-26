declare module 'react-native/Libraries/vendor/core/ErrorUtils' {
  type ErrorHandlerCallback = (error: unknown, isFatal?: boolean) => void;
  const ErrorUtils: {
    setGlobalHandler: (callback: ErrorHandlerCallback) => void;
    getGlobalHandler: () => ErrorHandlerCallback;
  };
  export default ErrorUtils;
}
