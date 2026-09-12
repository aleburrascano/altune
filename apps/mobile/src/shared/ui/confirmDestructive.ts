import { Alert } from 'react-native';

export interface ConfirmDestructiveOptions {
  title: string;
  message: string;
  confirmLabel: string;
  onConfirm: () => void;
}

/**
 * The two-button destructive-confirm dialog: a `cancel` button plus a
 * `destructive` confirm running `onConfirm`. One home for the `Alert.alert`
 * skeleton the library and queue screens otherwise rebuild inline.
 * Settings keeps its own in-app `ConfirmDialog` and does not route through here.
 */
export function confirmDestructive({
  title,
  message,
  confirmLabel,
  onConfirm,
}: ConfirmDestructiveOptions): void {
  Alert.alert(title, message, [
    { text: 'Cancel', style: 'cancel' },
    { text: confirmLabel, style: 'destructive', onPress: onConfirm },
  ]);
}
