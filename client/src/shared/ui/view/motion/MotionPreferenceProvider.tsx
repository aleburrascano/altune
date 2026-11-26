import { createContext, use, type ReactElement, type ReactNode } from 'react';

import type { MotionPreference } from '@shared/ui/ports/motionPreference';
import { platformMotionPreference } from '@shared/ui/view/motion/platformMotionPreference';

const MotionPreferenceContext = createContext<MotionPreference>(platformMotionPreference);

type MotionPreferenceProviderProps = { preference: MotionPreference; children: ReactNode };

export function MotionPreferenceProvider({
  preference,
  children,
}: MotionPreferenceProviderProps): ReactElement {
  return (
    <MotionPreferenceContext.Provider value={preference}>
      {children}
    </MotionPreferenceContext.Provider>
  );
}

export function useMotionPreference(): MotionPreference {
  return use(MotionPreferenceContext);
}
