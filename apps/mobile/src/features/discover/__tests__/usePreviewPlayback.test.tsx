import type { ReactNode } from 'react';

import { act, renderHook } from '@testing-library/react-native';
import * as Haptics from 'expo-haptics';

import { asTrackId } from '@shared/api-client/ids';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

import { usePreviewPlayback } from '../hooks/usePreviewPlayback';

jest.mock('expo-haptics', () => ({
  impactAsync: jest.fn().mockResolvedValue(undefined),
  ImpactFeedbackStyle: { Light: 'light' },
}));

const PREVIEW_URL = 'https://cdn.example/preview.mp3';

function resultFixture(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'The Title',
    subtitle: 'The Artist',
    image_url: 'https://img.example/a.jpg',
    confidence: 'high',
    sources: [{ provider: 'deezer', external_id: 'ext-1', url: 'https://x' }],
    extras: { preview_url: PREVIEW_URL },
    ...overrides,
  };
}

function makePlayback(overrides: Partial<PlaybackContextValue> = {}): PlaybackContextValue {
  return {
    status: 'idle',
    track: null,
    positionMs: 0,
    durationMs: 0,
    errorMessage: null,
    errorKind: null,
    play: jest.fn().mockResolvedValue(undefined),
    startQueue: jest.fn().mockResolvedValue(undefined),
    skipToQueueIndex: jest.fn().mockResolvedValue(undefined),
    reorderUpcoming: jest.fn().mockResolvedValue(undefined),
    appendToQueue: jest.fn().mockResolvedValue(undefined),
    insertNext: jest.fn().mockResolvedValue(undefined),
    skipNext: jest.fn().mockResolvedValue(undefined),
    skipPrevious: jest.fn().mockResolvedValue(undefined),
    removeQueueIndex: jest.fn().mockResolvedValue(undefined),
    pause: jest.fn(),
    resume: jest.fn(),
    seekTo: jest.fn(),
    setRate: jest.fn(),
    stop: jest.fn(),
    retry: jest.fn(),
    ...overrides,
  };
}

function playingPreview(previewUrl: string): Partial<PlaybackContextValue> {
  return {
    status: 'playing',
    track: { source: { kind: 'preview', previewUrl }, title: 't', artist: 'a', artworkUrl: null },
  };
}

function setup(result: DiscoveryResult, playback: PlaybackContextValue) {
  function Wrapper({ children }: { children: ReactNode }) {
    return <PlaybackContext.Provider value={playback}>{children}</PlaybackContext.Provider>;
  }
  return renderHook(() => usePreviewPlayback(result), { wrapper: Wrapper });
}

beforeEach(() => {
  jest.clearAllMocks();
});

describe('usePreviewPlayback', () => {
  it('reports no preview for a non-track result even when extras carry a preview url', () => {
    const { result } = setup(resultFixture({ kind: 'album' }), makePlayback());

    expect(result.current).toEqual({ hasPreview: false });
  });

  it('reports no preview for a track without a preview url', () => {
    const { result } = setup(resultFixture({ extras: {} }), makePlayback());

    expect(result.current).toEqual({ hasPreview: false });
  });

  it('is playing only when this exact preview is the playing track', () => {
    const mine = setup(resultFixture(), makePlayback(playingPreview(PREVIEW_URL)));
    const other = setup(
      resultFixture(),
      makePlayback(playingPreview('https://cdn.example/other.mp3')),
    );
    const paused = setup(
      resultFixture(),
      makePlayback({ ...playingPreview(PREVIEW_URL), status: 'paused' }),
    );
    const library = setup(
      resultFixture(),
      makePlayback({
        status: 'playing',
        track: {
          source: { kind: 'library', trackId: asTrackId('t1') },
          title: 't',
          artist: 'a',
          artworkUrl: null,
        },
      }),
    );

    expect(mine.result.current).toMatchObject({ hasPreview: true, isPlaying: true });
    expect(other.result.current).toMatchObject({ hasPreview: true, isPlaying: false });
    expect(paused.result.current).toMatchObject({ hasPreview: true, isPlaying: false });
    expect(library.result.current).toMatchObject({ hasPreview: true, isPlaying: false });
  });

  it('starts this preview with the result metadata and a light haptic when not playing', () => {
    const playback = makePlayback();
    const { result } = setup(resultFixture({ subtitle: null }), playback);

    act(() => {
      if (result.current.hasPreview) result.current.togglePreview();
    });

    expect(playback.play).toHaveBeenCalledWith({
      source: { kind: 'preview', previewUrl: PREVIEW_URL },
      title: 'The Title',
      artist: '',
      artworkUrl: 'https://img.example/a.jpg',
    });
    expect(playback.pause).not.toHaveBeenCalled();
    expect(Haptics.impactAsync).toHaveBeenCalledWith(Haptics.ImpactFeedbackStyle.Light);
  });

  it('pauses instead of restarting when this preview is already playing', () => {
    const playback = makePlayback(playingPreview(PREVIEW_URL));
    const { result } = setup(resultFixture(), playback);

    act(() => {
      if (result.current.hasPreview) result.current.togglePreview();
    });

    expect(playback.pause).toHaveBeenCalledTimes(1);
    expect(playback.play).not.toHaveBeenCalled();
    expect(Haptics.impactAsync).toHaveBeenCalledTimes(1);
  });
});
