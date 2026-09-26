import { confirm } from './dialog/dialog';

export interface ConfirmDestructiveOptions {
  title: string;
  message: string;
  confirmLabel: string;
  onConfirm: () => void;
}

export function confirmDestructive(options: ConfirmDestructiveOptions): void {
  confirm(options);
}
