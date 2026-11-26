import type { Navigator } from '@shared/navigation';

export type FocusEffect = (effect: () => void | (() => void)) => void;

export type DiscoverEnv = {
  navigator: Navigator;
  useFocus: FocusEffect;
  dismissKeyboard: () => void;
};
