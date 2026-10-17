import { AccessibilityInfo, Platform } from 'react-native';

import { announce } from '@shared/ui/a11y/announce';
import { announce as announceAndroid } from '@shared/ui/a11y/announce.android';

const flush = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

describe('announce', () => {
  let isEnabled: jest.SpyInstance;
  let spoken: jest.SpyInstance;

  beforeEach(() => {
    isEnabled = jest.spyOn(AccessibilityInfo, 'isScreenReaderEnabled').mockReset();
    spoken = jest
      .spyOn(AccessibilityInfo, 'announceForAccessibility')
      .mockReset()
      .mockImplementation(() => {});
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('ignores an empty message', () => {
    announce('');

    expect(isEnabled).not.toHaveBeenCalled();
  });

  it('announces when the screen reader is on', async () => {
    isEnabled.mockResolvedValue(true);

    announce('Saved');
    await flush();

    expect(spoken).toHaveBeenCalledTimes(1);
    expect(spoken).toHaveBeenCalledWith('Saved');
  });

  it('stays silent when the screen reader is off', async () => {
    isEnabled.mockResolvedValue(false);

    announce('Saved');
    await flush();

    expect(spoken).not.toHaveBeenCalled();
  });

  it('announces once across an on call followed by an off call', async () => {
    isEnabled.mockResolvedValueOnce(true).mockResolvedValueOnce(false);

    announce('First');
    announce('Second');
    await flush();

    expect(spoken).toHaveBeenCalledTimes(1);
    expect(spoken).toHaveBeenCalledWith('First');
  });

  it('is silent in the android file even with the reader on', async () => {
    isEnabled.mockResolvedValue(true);

    announceAndroid('Saved');
    await flush();

    expect(isEnabled).not.toHaveBeenCalled();
    expect(spoken).not.toHaveBeenCalled();
  });

  it('announces from the base file when Platform.OS is android', async () => {
    const original = Platform.OS;
    Platform.OS = 'android';
    isEnabled.mockResolvedValue(true);

    try {
      announce('Saved');
      await flush();
    } finally {
      Platform.OS = original;
    }

    expect(spoken).toHaveBeenCalledWith('Saved');
  });
});
