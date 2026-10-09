export interface ConfirmOptions {
  title: string;
  message: string;
  confirmLabel: string;
  onConfirm: () => void;
}

export function showAlert(title: string, message: string): void {
  window.alert(`${title}\n\n${message}`);
}

export function confirmDestructive({ title, message, onConfirm }: ConfirmOptions): void {
  if (window.confirm(`${title}\n\n${message}`)) onConfirm();
}
