import type { FailureAlert } from '@shared/ui';

import { failureTail, type LibraryFailure } from '@features/library/domain/state';

export function libraryFailureAlert(
  surface: string,
  title: string,
  lead: string,
  failure: LibraryFailure,
): FailureAlert {
  return { surface, title, message: `${lead} ${failureTail(failure)}` };
}
