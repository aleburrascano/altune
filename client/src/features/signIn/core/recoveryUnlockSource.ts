export interface RecoveryUnlockSource {
  subscribe: (listener: () => void) => () => void;
  isUnlocked: (userId: string | null) => boolean;
  clear: () => void;
}
