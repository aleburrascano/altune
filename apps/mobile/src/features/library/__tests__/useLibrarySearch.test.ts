/**
 * useLibrarySearch — the committed query must always track the input box.
 *
 * Regression: onChangeText handled length 0 (clear) and length >= 2 (commit)
 * but NOT length 1 — deleting "keep on lov" down to "k" kept the old committed
 * query silently filtering the whole library while the box showed one
 * character. A persistently-filtered library reads as "my library is gone".
 */
import { act, renderHook } from '@testing-library/react-native';

import { useLibrarySearch } from '../hooks/useLibrarySearch';
import type { TrackResponse } from '../../../shared/api-client/types';

jest.useFakeTimers();

function _track(title: string, artist: string): TrackResponse {
  return {
    id: `${title}-${artist}`,
    title,
    artist,
    album: null,
    duration_seconds: null,
    added_at: '2026-07-01T12:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    failure_reason: null,
  };
}

const LIBRARY = [_track('Keep On Loving You', 'REO Speedwagon'), _track('Africa', 'Toto')];

describe('useLibrarySearch', () => {
  it('commits the query after the debounce and filters', () => {
    const { result } = renderHook(() => useLibrarySearch());

    act(() => result.current.onChangeText('toto'));
    act(() => jest.advanceTimersByTime(300));

    expect(result.current.hasQuery).toBe(true);
    expect(result.current.filter(LIBRARY).map((t) => t.title)).toEqual(['Africa']);
  });

  it('clears the committed query when the input drops below two characters', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('keep on lov'));
    act(() => jest.advanceTimersByTime(300));
    expect(result.current.filter(LIBRARY)).toHaveLength(1);

    // Delete down to a single character — the filter must lift immediately,
    // not keep applying "keep on lov" behind a box that shows "k".
    act(() => result.current.onChangeText('k'));

    expect(result.current.hasQuery).toBe(false);
    expect(result.current.filter(LIBRARY)).toHaveLength(2);
  });

  it('onClear resets input and committed query', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('toto'));
    act(() => jest.advanceTimersByTime(300));

    act(() => result.current.onClear());

    expect(result.current.inputValue).toBe('');
    expect(result.current.hasQuery).toBe(false);
    expect(result.current.filter(LIBRARY)).toHaveLength(2);
  });

  it('exposes the committed query for the no-results message', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('reo speedwagon'));
    act(() => jest.advanceTimersByTime(300));

    expect(result.current.query).toBe('reo speedwagon');
  });
});
