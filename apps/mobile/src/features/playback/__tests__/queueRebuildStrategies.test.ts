import { asTrackId } from '@shared/api-client/ids';
import type { QueueStateResponse } from '@shared/api-client/playback';
import type { AcquisitionStatus, TrackResponse } from '@shared/api-client/types';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { _resetPlaybackHealthForTest, flushPlaybackHealth } from '../playbackHealth';
import {
  rebuildFromNaturalOrder,
  rebuildFromPlayOrderAlone,
  rebuildOnFirstWorkingRung,
  showSavedTrackWhileRehydrating,
} from '../queueRebuildStrategies';

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const recordEventMock = recordEvent as jest.MockedFunction<typeof recordEvent>;

const INITIAL_STATE = useQueueStore.getState();

function track(id: string, status: AcquisitionStatus = 'ready'): TrackResponse {
  return {
    id: asTrackId(id),
    title: `Track ${id}`,
    artist: 'Artist',
    album: null,
    duration_seconds: 200,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: status,
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

function saved(overrides: Partial<QueueStateResponse> = {}): QueueStateResponse {
  return {
    track_ids: [],
    current_index: 0,
    position_ms: 0,
    shuffled: false,
    repeat_mode: 'off',
    source: null,
    natural_order: [],
    ...overrides,
  };
}

function mapOf(...tracks: TrackResponse[]): Map<string, TrackResponse> {
  return new Map(tracks.map((t) => [t.id, t]));
}

function orderedIds(): string[] {
  return orderedQueueTracks(useQueueStore.getState()).map((t) =>
    t.source.kind === 'library' ? t.source.trackId : '',
  );
}

beforeEach(() => {
  useQueueStore.setState(INITIAL_STATE, true);
  _resetPlaybackHealthForTest();
  recordEventMock.mockReset().mockResolvedValue(undefined);
});

describe('showSavedTrackWhileRehydrating', () => {
  it('does nothing and returns null when there is no ready current track', () => {
    expect(showSavedTrackWhileRehydrating(saved())).toBeNull();
    const pending = saved({
      current_track: {
        id: 'a',
        title: 'A',
        artist: 'X',
        artwork_url: null,
        duration_seconds: null,
        acquisition_status: 'pending',
      },
    });
    expect(showSavedTrackWhileRehydrating(pending)).toBeNull();
    expect(useQueueStore.getState().tracks).toHaveLength(0);
  });

  it('loads the saved track alone with its position and returns the new generation', () => {
    const generation = showSavedTrackWhileRehydrating(
      saved({
        position_ms: 4200,
        source: { kind: 'search', query: 'q' },
        current_track: {
          id: 'a',
          title: 'A',
          artist: 'X',
          artwork_url: null,
          duration_seconds: 10,
          acquisition_status: 'ready',
        },
      }),
    );
    const s = useQueueStore.getState();
    expect(generation).toBe(s.generation);
    expect(orderedIds()).toEqual(['a']);
    expect(s.resumePositionMs).toBe(4200);
    expect(s.source).toEqual({ kind: 'search', query: 'q' });
  });
});

describe('rebuildFromNaturalOrder', () => {
  const isReadyIn = (m: Map<string, TrackResponse>) => (id: string) =>
    m.get(id)?.acquisition_status === 'ready';

  it('returns false without a saved natural order', () => {
    const m = mapOf(track('a'));
    expect(rebuildFromNaturalOrder(saved({ track_ids: ['a'] }), m, isReadyIn(m), null)).toBe(false);
  });

  it('returns false when no natural-order track is still ready', () => {
    const m = mapOf(track('a', 'failed'));
    const s = saved({ track_ids: ['a'], natural_order: ['a'] });
    expect(rebuildFromNaturalOrder(s, m, isReadyIn(m), null)).toBe(false);
  });

  it('restores the shuffled play order over the natural order, skipping unready tracks', () => {
    const m = mapOf(track('a'), track('b', 'pending'), track('c'), track('d'));
    const s = saved({
      natural_order: ['a', 'b', 'c', 'd'],
      track_ids: ['d', 'b', 'a', 'c'],
      current_index: 2,
      shuffled: true,
    });
    expect(rebuildFromNaturalOrder(s, m, isReadyIn(m), { kind: 'library' })).toBe(true);
    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => (t.source.kind === 'library' ? t.source.trackId : ''))).toEqual([
      'a',
      'c',
      'd',
    ]);
    expect(orderedIds()).toEqual(['d', 'a', 'c']);
    expect(state.currentTrack()?.title).toBe('Track a');
    expect(state.shuffled).toBe(true);
    expect(state.source).toEqual({ kind: 'library' });
  });
});

describe('rebuildFromPlayOrderAlone', () => {
  it('returns false when no saved track is ready', () => {
    const m = mapOf(track('a', 'pending'));
    expect(rebuildFromPlayOrderAlone(saved({ track_ids: ['a', 'zz'] }), m, null)).toBe(false);
  });

  it('loads the ready tracks in saved order and follows the current track by identity', () => {
    const m = mapOf(track('a'), track('b', 'failed'), track('c'));
    const s = saved({ track_ids: ['a', 'b', 'c'], current_index: 2 });
    expect(rebuildFromPlayOrderAlone(s, m, null)).toBe(true);
    const state = useQueueStore.getState();
    expect(orderedIds()).toEqual(['a', 'c']);
    expect(state.currentTrack()?.title).toBe('Track c');
    expect(state.shuffled).toBe(false);
  });

  it('marks the queue shuffled when the saved state was shuffled', () => {
    const m = mapOf(track('a'), track('b'));
    expect(
      rebuildFromPlayOrderAlone(saved({ track_ids: ['b', 'a'], shuffled: true }), m, null),
    ).toBe(true);
    expect(useQueueStore.getState().shuffled).toBe(true);
  });
});

// Regression (#1727): every rung of the ladder restores a queue (or nothing) and returns
// quietly, so a resume that degraded to the play-order rung, or exhausted the ladder, looked
// exactly like a clean one in telemetry.
describe('rebuildOnFirstWorkingRung', () => {
  const isReadyIn = (m: Map<string, TrackResponse>) => (id: string) =>
    m.get(id)?.acquisition_status === 'ready';

  function reportedTally(): Record<string, unknown> {
    flushPlaybackHealth();
    expect(recordEventMock).toHaveBeenCalledTimes(1);
    const event = recordEventMock.mock.calls[0]?.[0];
    expect(event?.type).toBe('playback_health');
    return event?.payload ?? {};
  }

  it('records the natural-order rung when the saved natural order rebuilds', () => {
    const m = mapOf(track('a'), track('b'));
    const s = saved({ natural_order: ['a', 'b'], track_ids: ['b', 'a'], shuffled: true });

    expect(rebuildOnFirstWorkingRung(s, m, isReadyIn(m), null)).toBe('natural');

    expect(orderedIds()).toEqual(['b', 'a']);
    expect(reportedTally()).toMatchObject({
      queue_rebuild_natural: 1,
      queue_rebuild_play_order: 0,
      queue_rebuild_exhausted: 0,
    });
  });

  it('records the degraded rung when only the play order is left to rebuild from', () => {
    const m = mapOf(track('a'), track('b'));
    const s = saved({ natural_order: [], track_ids: ['b', 'a'] });

    expect(rebuildOnFirstWorkingRung(s, m, isReadyIn(m), null)).toBe('play_order');

    expect(orderedIds()).toEqual(['b', 'a']);
    expect(reportedTally()).toMatchObject({
      queue_rebuild_natural: 0,
      queue_rebuild_play_order: 1,
      queue_rebuild_exhausted: 0,
    });
  });

  it('records an exhausted ladder when no rung can rebuild anything', () => {
    const m = mapOf(track('a', 'pending'));
    const s = saved({ natural_order: ['a'], track_ids: ['a'] });

    expect(rebuildOnFirstWorkingRung(s, m, isReadyIn(m), null)).toBe('exhausted');

    expect(useQueueStore.getState().tracks).toHaveLength(0);
    expect(reportedTally()).toMatchObject({
      queue_rebuild_natural: 0,
      queue_rebuild_play_order: 0,
      queue_rebuild_exhausted: 1,
    });
  });
});
