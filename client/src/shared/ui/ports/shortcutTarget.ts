type KeydownListener = (event: KeyboardEvent) => void;
type KeydownSubscription = (type: 'keydown', listener: KeydownListener) => void;

export interface ShortcutTarget {
  addEventListener?: KeydownSubscription;
  removeEventListener?: KeydownSubscription;
}
