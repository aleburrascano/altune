import { PLAYBACK_RATES, rateLabel, usePlaybackRateStore } from '../playbackRateStore';

afterEach(() => {
  usePlaybackRateStore.setState({ rate: 1 });
});

describe('rateLabel — the chip label for a playback rate', () => {
  it('renders each offered rate with a trailing multiplier sign', () => {
    expect(PLAYBACK_RATES.map(rateLabel)).toEqual([
      '0.75×',
      '1×',
      '1.25×',
      '1.5×',
      '1.75×',
      '2×',
    ]);
  });
});

describe('usePlaybackRateStore — the selected rate', () => {
  it('defaults to normal speed', () => {
    expect(usePlaybackRateStore.getState().rate).toBe(1);
  });

  it('replaces the rate when a new one is set', () => {
    usePlaybackRateStore.getState().setRate(1.5);

    expect(usePlaybackRateStore.getState().rate).toBe(1.5);
  });
});
