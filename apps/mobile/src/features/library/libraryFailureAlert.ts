import { showAlert } from '@shared/ui/dialog/dialog';

import { failureTail, type LibraryFailure } from './state';

export function alertLibraryFailure(title: string, lead: string, failure: LibraryFailure): void {
  showAlert(title, `${lead} ${failureTail(failure)}`);
}
