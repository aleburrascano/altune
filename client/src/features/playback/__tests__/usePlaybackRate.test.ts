import { renderHook } from '@testing-library/react-native';
import React from 'react';

import { PlaybackContext } from '@shared/playback/usePlayback';
import { makeIdlePlayback } from '@testing/factories';

import {
  PLAYBACK_RATES,
  usePlaybackRateStore,
  useSpeedOptions,
} from '@features/playback/hooks/usePlaybackRate';

type ProviderProps = { children: React.ReactNode };

function chipLabelAt(rate: number): string {
  usePlaybackRateStore.setState({ rate });
  const playback = makeIdlePlayback();
  function Wrapper({ children }: ProviderProps): React.ReactElement {
    return React.createElement(PlaybackContext.Provider, { value: playback }, children);
  }
  return renderHook(() => useSpeedOptions(), { wrapper: Wrapper }).result.current.valueLabel;
}

afterEach(() => {
  usePlaybackRateStore.setState({ rate: 1 });
});

describe('rateLabel — the chip label for a playback rate', () => {
  it('renders each offered rate with a trailing multiplier sign', () => {
    expect(PLAYBACK_RATES.map(chipLabelAt)).toEqual([
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
