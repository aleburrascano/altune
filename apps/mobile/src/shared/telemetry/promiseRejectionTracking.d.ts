declare module 'promise/setimmediate/rejection-tracking' {
  export type RejectionTrackingOptions = {
    allRejections?: boolean;
    onUnhandled?: (id: number, error: unknown) => void;
    onHandled?: (id: number) => void;
  };
  export function enable(options?: RejectionTrackingOptions): void;
  export function disable(): void;
}
