import { showFailureAlert } from '@shared/ui';

import { failureTail, type LibraryFailure } from '@features/library/domain/state';

export function alertLibraryFailure(
  surface: string,
  title: string,
  lead: string,
  failure: LibraryFailure,
): void {
  showFailureAlert({ surface, title, message: `${lead} ${failureTail(failure)}` });
}
