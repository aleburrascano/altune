import { createContext, use, type ReactNode } from 'react';

import type { TapFeedback } from '@shared/ui/ports/tapFeedback';

type TapFeedbackProviderProps = { feedback: TapFeedback; children: ReactNode };

function silentTapFeedback(): void {}

const TapFeedbackContext = createContext<TapFeedback>(silentTapFeedback);

export function TapFeedbackProvider({ feedback, children }: TapFeedbackProviderProps) {
  return <TapFeedbackContext value={feedback}>{children}</TapFeedbackContext>;
}

export function useTapFeedback(): TapFeedback {
  return use(TapFeedbackContext);
}
