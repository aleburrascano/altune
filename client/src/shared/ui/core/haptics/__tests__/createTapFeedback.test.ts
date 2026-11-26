import { createTapFeedback, tapFeedback, type TapDevice } from '@shared/ui';

type RecordingDevice = { device: TapDevice; calls: string[] };

function recordingDevice(): RecordingDevice {
  const calls: string[] = [];
  return {
    calls,
    device: {
      impact: (style) => calls.push(`impact:${style}`),
      selection: () => calls.push('selection'),
    },
  };
}

describe('createTapFeedback', () => {
  it('taps light by default', () => {
    const { device, calls } = recordingDevice();

    createTapFeedback(device)();

    expect(calls).toEqual(['impact:light']);
  });

  it('taps the matching impact style for a medium tap', () => {
    const { device, calls } = recordingDevice();

    createTapFeedback(device)('medium');

    expect(calls).toEqual(['impact:medium']);
  });

  it('selects, without an impact, for a selection tap', () => {
    const { device, calls } = recordingDevice();

    createTapFeedback(device)('selection');

    expect(calls).toEqual(['selection']);
  });

  it('follows the kind across consecutive calls', () => {
    const { device, calls } = recordingDevice();
    const feedback = createTapFeedback(device);

    feedback('selection');
    feedback('medium');

    expect(calls).toEqual(['selection', 'impact:medium']);
  });
});

describe('the app tapFeedback', () => {
  it('can be called for every kind without throwing', () => {
    expect(() => {
      tapFeedback();
      tapFeedback('medium');
      tapFeedback('selection');
    }).not.toThrow();
  });
});
