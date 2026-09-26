import type * as Haptics from 'expo-haptics';

import type { tapFeedback as tapFeedbackType } from '../haptics';

jest.mock('expo-haptics', () => ({
  impactAsync: jest.fn().mockResolvedValue(undefined),
  selectionAsync: jest.fn().mockResolvedValue(undefined),
  ImpactFeedbackStyle: { Light: 'light', Medium: 'medium' },
}));

function runOn(
  os: string,
  run: (tapFeedback: typeof tapFeedbackType, haptics: typeof Haptics) => void,
): void {
  jest.isolateModules(() => {
    require('react-native').Platform.OS = os;
    const loadedHaptics: typeof Haptics = require('expo-haptics');
    const { tapFeedback }: { tapFeedback: typeof tapFeedbackType } = require('../haptics');
    run(tapFeedback, loadedHaptics);
  });
}

describe('tapFeedback on web', () => {
  it('never calls expo-haptics for the default kind', () => {
    runOn('web', (tapFeedback, Haptics) => {
      tapFeedback();

      expect(Haptics.impactAsync).not.toHaveBeenCalled();
      expect(Haptics.selectionAsync).not.toHaveBeenCalled();
    });
  });

  it('never calls expo-haptics for a selection tap', () => {
    runOn('web', (tapFeedback, Haptics) => {
      tapFeedback('selection');

      expect(Haptics.selectionAsync).not.toHaveBeenCalled();
      expect(Haptics.impactAsync).not.toHaveBeenCalled();
    });
  });

  it('never calls expo-haptics for a medium tap', () => {
    runOn('web', (tapFeedback, Haptics) => {
      tapFeedback('medium');

      expect(Haptics.impactAsync).not.toHaveBeenCalled();
    });
  });
});

describe('tapFeedback on native', () => {
  it('calls impactAsync with the matching style on ios', () => {
    runOn('ios', (tapFeedback, Haptics) => {
      tapFeedback('medium');

      expect(Haptics.impactAsync).toHaveBeenCalledWith(Haptics.ImpactFeedbackStyle.Medium);
    });
  });

  it('calls selectionAsync for a selection tap on android', () => {
    runOn('android', (tapFeedback, Haptics) => {
      tapFeedback('selection');

      expect(Haptics.selectionAsync).toHaveBeenCalledTimes(1);
      expect(Haptics.impactAsync).not.toHaveBeenCalled();
    });
  });
});
