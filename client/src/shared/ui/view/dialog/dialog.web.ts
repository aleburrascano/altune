import type { Dialogs } from '@shared/ui/ports/dialogs';

export const platformDialogs: Dialogs = {
  alert: (title, message) => {
    window.alert(`${title}\n\n${message}`);
  },
  confirmDestructive: ({ title, message, onConfirm }) => {
    if (window.confirm(`${title}\n\n${message}`)) onConfirm();
  },
};
