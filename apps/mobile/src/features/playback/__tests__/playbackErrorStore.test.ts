import { renderHook } from '@testing-library/react-native';

import {
  clearPlaybackError,
  reportPlaybackError,
  usePlaybackErrorFor,
  usePlaybackErrorStore,
} from '../playbackErrorStore';

afterEach(() => {
  usePlaybackErrorStore.getState().clear();
});

describe('playbackErrorStore — recording and clearing a track error', () => {
  it('records the failing key and its message', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    expect(usePlaybackErrorStore.getState().key).toBe('library:trk-1');
    expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
  });

  it('clears both the key and the message', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    clearPlaybackError();

    expect(usePlaybackErrorStore.getState().key).toBeNull();
    expect(usePlaybackErrorStore.getState().message).toBeNull();
  });
});

describe('usePlaybackErrorFor — the message a given track should show', () => {
  it('returns the message when the reported key matches', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor('library:trk-1'));

    expect(result.current).toBe('Could not load this track');
  });

  it('returns null for a different track than the one that failed', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor('library:trk-2'));

    expect(result.current).toBeNull();
  });

  it('returns null when the queried key is null', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor(null));

    expect(result.current).toBeNull();
  });
});
