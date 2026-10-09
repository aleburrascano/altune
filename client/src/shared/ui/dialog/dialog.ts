import { Alert } from 'react-native';

export interface ConfirmOptions {
  title: string;
  message: string;
  confirmLabel: string;
  onConfirm: () => void;
}

export function showAlert(title: string, message: string): void {
  Alert.alert(title, message);
}

export function confirm({ title, message, confirmLabel, onConfirm }: ConfirmOptions): void {
  Alert.alert(title, message, [
    { text: 'Cancel', style: 'cancel' },
    { text: confirmLabel, style: 'destructive', onPress: onConfirm },
  ]);
}
