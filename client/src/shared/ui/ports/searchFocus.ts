import type { Navigator } from '@shared/navigation';

export interface SearchFocus {
  register: (focus: () => void) => () => void;
  requestFocus: (navigator: Pick<Navigator, 'push'>) => void;
}
