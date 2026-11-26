import type { Navigator } from '@shared/navigation';
import type { DiscoverEnv, FocusEffect } from '@features/discovery';

export type RecordingNavigator = Navigator & { push: jest.Mock };

export type FocusControl = { useFocus: FocusEffect; focus: () => void };

export type TestEnv = {
  env: DiscoverEnv;
  navigator: RecordingNavigator;
  dismissKeyboard: jest.Mock;
  focus: () => void;
};

function recordingNavigator(): RecordingNavigator {
  return { push: jest.fn(), replace: jest.fn(), back: jest.fn(), canGoBack: () => false };
}

function focusControl(): FocusControl {
  const effects: (() => void | (() => void))[] = [];
  function useFocus(effect: () => void | (() => void)): void {
    if (!effects.includes(effect)) effects.push(effect);
  }
  return {
    useFocus,
    focus: () => {
      effects.forEach((effect) => effect());
    },
  };
}

export function discoverEnv(): TestEnv {
  const navigator = recordingNavigator();
  const dismissKeyboard = jest.fn();
  const { useFocus, focus } = focusControl();
  return { env: { navigator, useFocus, dismissKeyboard }, navigator, dismissKeyboard, focus };
}
