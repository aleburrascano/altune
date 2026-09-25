// react-native ships ErrorUtils.d.ts as a bare `export interface ErrorUtils { ... }`, with no
// declared default export, even though the runtime module (ErrorUtils.js) exports the handler
// object as its default. This fills that gap for the one path we import it from.
declare module 'react-native/Libraries/vendor/core/ErrorUtils' {
  type ErrorHandlerCallback = (error: unknown, isFatal?: boolean) => void;
  const ErrorUtils: {
    setGlobalHandler: (callback: ErrorHandlerCallback) => void;
    getGlobalHandler: () => ErrorHandlerCallback;
  };
  export default ErrorUtils;
}
