import React from 'react';
import { renderHook } from '@testing-library/react-native';

import { usePlayback } from '../usePlayback';
import { PlaybackContext } from '../PlaybackContext';
import type { PlaybackContextValue } from '../types';

function buildContextValue(): PlaybackContextValue {
  return {
    status: 'playing',
    track: {
      source: { kind: 'library', trackId: 'track-1' },
      title: 'Test Track',
      artist: 'Test Artist',
      artworkUrl: null,
    },
    positionMs: 1_000,
    durationMs: 200_000,
    errorMessage: null,
    play: jest.fn(),
    startQueue: jest.fn(),
    skipToQueueIndex: jest.fn(),
    reorderUpcoming: jest.fn(),
    appendToQueue: jest.fn(),
    insertNext: jest.fn(),
    skipNext: jest.fn(),
    skipPrevious: jest.fn(),
    removeQueueIndex: jest.fn(),
    pause: jest.fn(),
    resume: jest.fn(),
    seekTo: jest.fn(),
    setRate: jest.fn(),
    stop: jest.fn(),
    retry: jest.fn(),
  };
}

describe('usePlayback', () => {
  it('returns the exact value supplied by the nearest PlaybackContext provider', () => {
    const value = buildContextValue();
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>
    );

    const { result } = renderHook(() => usePlayback(), { wrapper });

    expect(result.current).toBe(value);
  });

  it('throws when rendered outside a PlaybackContext provider', () => {
    expect(() => renderHook(() => usePlayback())).toThrow();
  });
});

function loadIsExpoGo(executionEnvironment: string): boolean {
  let isExpoGo: boolean | undefined;
  jest.isolateModules(() => {
    jest.doMock('expo-constants', () => ({
      __esModule: true,
      default: { executionEnvironment },
      ExecutionEnvironment: { Bare: 'bare', Standalone: 'standalone', StoreClient: 'storeClient' },
    }));
    ({ isExpoGo } = require('../isExpoGo') as { isExpoGo: boolean });
  });
  return isExpoGo as boolean;
}

describe('isExpoGo', () => {
  it('is true when the execution environment is the Expo Go store client', () => {
    expect(loadIsExpoGo('storeClient')).toBe(true);
  });

  it('is false when the execution environment is a standalone build', () => {
    expect(loadIsExpoGo('standalone')).toBe(false);
  });
});
