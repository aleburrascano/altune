import React from 'react';
import { renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';

import { usePlayback } from '../usePlayback';
import { PlaybackContext } from '../PlaybackContext';
import type { PlaybackContextValue } from '../types';

function buildContextValue(): PlaybackContextValue {
  return {
    status: 'playing',
    track: {
      source: { kind: 'library', trackId: asTrackId('track-1') },
      title: 'Test Track',
      artist: 'Test Artist',
      artworkUrl: null,
    },
    positionMs: 1_000,
    durationMs: 200_000,
    errorMessage: null,
    errorKind: null,
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
