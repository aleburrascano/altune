import type { AlertButton } from 'react-native';

import type { ConfirmOptions, Dialogs } from '@shared/ui/ports/dialogs';

export interface NativeAlert {
  alert(title: string, message: string, buttons?: AlertButton[]): void;
}

export function createNativeDialogs(native: NativeAlert): Dialogs {
  return {
    alert: (title, message) => {
      native.alert(title, message);
    },
    confirmDestructive: ({ title, message, confirmLabel, onConfirm }: ConfirmOptions) => {
      native.alert(title, message, [
        { text: 'Cancel', style: 'cancel' },
        { text: confirmLabel, style: 'destructive', onPress: onConfirm },
      ]);
    },
  };
}
