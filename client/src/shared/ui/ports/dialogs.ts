export interface ConfirmOptions {
  title: string;
  message: string;
  confirmLabel: string;
  onConfirm: () => void;
}

export interface Dialogs {
  alert(title: string, message: string): void;
  confirmDestructive(options: ConfirmOptions): void;
}
