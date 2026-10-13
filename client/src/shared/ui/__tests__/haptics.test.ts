import type * as Haptics from 'expo-haptics';

import type { tapFeedback as tapFeedbackType } from '@shared/ui/haptics';

import { impactAsync, selectionAsync } from 'expo-haptics';

import { tapFeedback as tapFeedbackWeb } from '@shared/ui/haptics.web';

jest.mock('expo-haptics', () => ({
  impactAsync: jest.fn().mockResolvedValue(undefined),
  selectionAsync: jest.fn().mockResolvedValue(undefined),
  ImpactFeedbackStyle: { Light: 'light', Medium: 'medium' },
}));

beforeEach(() => jest.clearAllMocks());

function runOn(
  os: string,
  run: (tapFeedback: typeof tapFeedbackType, haptics: typeof Haptics) => void,
): void {
  jest.isolateModules(() => {
    require('react-native').Platform.OS = os;
    const loadedHaptics: typeof Haptics = require('expo-haptics');
    const { tapFeedback }: { tapFeedback: typeof tapFeedbackType } = require('@shared/ui/haptics');
    run(tapFeedback, loadedHaptics);
  });
}

describe('tapFeedback on web', () => {
  it('never calls expo-haptics for the default kind', () => {
    tapFeedbackWeb();

    expect(impactAsync).not.toHaveBeenCalled();
    expect(selectionAsync).not.toHaveBeenCalled();
  });

  it('never calls expo-haptics for a selection tap', () => {
    tapFeedbackWeb('selection');

    expect(selectionAsync).not.toHaveBeenCalled();
    expect(impactAsync).not.toHaveBeenCalled();
  });

  it('never calls expo-haptics for a medium tap', () => {
    tapFeedbackWeb('medium');

    expect(impactAsync).not.toHaveBeenCalled();
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

  it('picks the behaviour from the file, not the runtime flag', () => {
    runOn('web', (tapFeedback, Haptics) => {
      tapFeedback('light');

      expect(Haptics.impactAsync).toHaveBeenCalledTimes(1);
      expect(Haptics.impactAsync).toHaveBeenCalledWith(Haptics.ImpactFeedbackStyle.Light);
    });
  });

  it('follows the kind across consecutive calls', () => {
    runOn('ios', (tapFeedback, Haptics) => {
      tapFeedback('selection');
      tapFeedback('medium');

      expect(Haptics.selectionAsync).toHaveBeenCalledTimes(1);
      expect(Haptics.impactAsync).toHaveBeenCalledTimes(1);
      expect(Haptics.impactAsync).toHaveBeenCalledWith(Haptics.ImpactFeedbackStyle.Medium);
    });
  });
});
