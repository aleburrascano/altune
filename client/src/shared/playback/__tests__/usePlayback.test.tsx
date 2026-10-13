import React from 'react';
import { renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/apiClient/ids';

import { usePlayback, PlaybackContext } from '@shared/playback/usePlayback';
import type { PlaybackContextValue } from '@shared/playback/playbackContextValue';
import { makeIdlePlayback } from '@testing/factories';

function buildContextValue(): PlaybackContextValue {
  return makeIdlePlayback({
    status: 'playing',
    track: {
      source: { kind: 'library', trackId: asTrackId('track-1') },
      title: 'Test Track',
      artist: 'Test Artist',
      artworkUrl: null,
    },
    positionMs: 1_000,
    durationMs: 200_000,
  });
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
