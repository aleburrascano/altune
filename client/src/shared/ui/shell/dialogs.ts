import type { ConfirmOptions, Dialogs } from '@shared/ui/ports/dialogs';

let installed!: Dialogs;

export function installDialogs(next: Dialogs): void {
  installed = next;
}

export function showAlert(title: string, message: string): void {
  installed.alert(title, message);
}

export function confirmDestructive(options: ConfirmOptions): void {
  installed.confirmDestructive(options);
}
