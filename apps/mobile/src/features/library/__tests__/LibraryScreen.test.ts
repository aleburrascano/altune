/**
 * `_viewForState` — state-machine helper for LibraryScreen.
 *
 * Slice 10 of view-library. The pure helper carries the load-bearing
 * decisions (which testID renders, which state is "designed" per AC#5/AC#6);
 * the JSX wrapping is straightforward.
 *
 * AIDEV-NOTE: RNTL-based component tests (rendering, retry click handling)
 * are deferred until the jest-expo workspace blocker is resolved (see
 * jest.config.js).
 */

import { _viewForState } from '../state';
import type { TrackResponse } from '../../../shared/api-client/types';

function _state(args: {
  isLoading?: boolean;
  error?: Error | null;
  items?: TrackResponse[];
}): { isLoading: boolean; error: Error | null; items: TrackResponse[] } {
  return {
    isLoading: args.isLoading ?? false,
    error: args.error ?? null,
    items: args.items ?? [],
  };
}

const _aTrack: TrackResponse = {
  id: '11111111-1111-1111-1111-111111111111',
  title: 'T',
  artist: 'A',
  album: null,
  duration_seconds: null,
  added_at: '2026-05-01T12:00:00Z',
};

describe('_viewForState', () => {
  it('returns "loading" when isLoading is true', () => {
    expect(_viewForState(_state({ isLoading: true }))).toBe('loading');
  });

  it('returns "error" when error is set (and not loading)', () => {
    expect(_viewForState(_state({ error: new Error('boom') }))).toBe('error');
  });

  it('returns "empty" when there are no items, no error, not loading', () => {
    expect(_viewForState(_state({ items: [] }))).toBe('empty');
  });

  it('returns "list" when items are present', () => {
    expect(_viewForState(_state({ items: [_aTrack] }))).toBe('list');
  });

  it('prefers loading over error (loading is the early state)', () => {
    expect(_viewForState(_state({ isLoading: true, error: new Error('x') }))).toBe('loading');
  });

  it('prefers error over empty (error is a real failure to surface)', () => {
    expect(_viewForState(_state({ error: new Error('boom'), items: [] }))).toBe('error');
  });
});
