import { createExpoTapDevice, type HapticsLib } from '@shared/ui';

type FakeHaptics = { haptics: HapticsLib; calls: string[] };

function fakeHaptics(): FakeHaptics {
  const calls: string[] = [];
  return {
    calls,
    haptics: {
      ImpactFeedbackStyle: { Light: 'Light', Medium: 'Medium' },
      impactAsync: (style) => {
        calls.push(`impact:${String(style)}`);
        return Promise.resolve();
      },
      selectionAsync: () => {
        calls.push('selection');
        return Promise.resolve();
      },
    },
  };
}

describe('createExpoTapDevice', () => {
  it('plays the matching impact and selection on a native device', () => {
    const { haptics, calls } = fakeHaptics();
    const device = createExpoTapDevice(haptics, () => false);

    device.impact('light');
    device.impact('medium');
    device.selection();

    expect(calls).toEqual(['impact:Light', 'impact:Medium', 'selection']);
  });

  it('plays nothing on web, where expo-haptics would vibrate or click a hidden switch', () => {
    const { haptics, calls } = fakeHaptics();
    const device = createExpoTapDevice(haptics, () => true);

    device.impact('light');
    device.selection();

    expect(calls).toEqual([]);
  });
});
