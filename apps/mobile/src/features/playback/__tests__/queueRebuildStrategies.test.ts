import { asTrackId } from '@shared/api-client/ids';
import type { QueueStateResponse } from '@shared/api-client/playback';
import type { AcquisitionStatus, TrackResponse } from '@shared/api-client/types';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { _resetPlaybackHealthForTest, flushPlaybackHealth } from '../playbackHealth';
import {
  rebuildOnFirstWorkingRungReportingCurrent,
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

describe('natural-order rung', () => {
  const isReadyIn = (m: Map<string, TrackResponse>) => (id: string) =>
    m.get(id)?.acquisition_status === 'ready';

  it('skips the natural rung without a saved natural order', () => {
    const m = mapOf(track('a'));
    const s = saved({ track_ids: ['a'] });
    expect(rebuildOnFirstWorkingRungReportingCurrent(s, m, isReadyIn(m), null).rung).toBe(
      'play_order',
    );
  });

  it('exhausts when no natural-order track is still ready', () => {
    const m = mapOf(track('a', 'failed'));
    const s = saved({ track_ids: ['a'], natural_order: ['a'] });
    expect(rebuildOnFirstWorkingRungReportingCurrent(s, m, isReadyIn(m), null).rung).toBe(
      'exhausted',
    );
  });

  it('restores the shuffled play order over the natural order, skipping unready tracks', () => {
    const m = mapOf(track('a'), track('b', 'pending'), track('c'), track('d'));
    const s = saved({
      natural_order: ['a', 'b', 'c', 'd'],
      track_ids: ['d', 'b', 'a', 'c'],
      current_index: 2,
      shuffled: true,
    });
    const outcome = rebuildOnFirstWorkingRungReportingCurrent(s, m, isReadyIn(m), {
      kind: 'library',
    });
    expect(outcome.rung).toBe('natural');
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

describe('play-order rung', () => {
  const neverReady = () => false;

  it('exhausts when no saved track is ready', () => {
    const m = mapOf(track('a', 'pending'));
    const s = saved({ track_ids: ['a', 'zz'] });
    expect(rebuildOnFirstWorkingRungReportingCurrent(s, m, neverReady, null).rung).toBe(
      'exhausted',
    );
  });

  it('loads the ready tracks in saved order and follows the current track by identity', () => {
    const m = mapOf(track('a'), track('b', 'failed'), track('c'));
    const s = saved({ track_ids: ['a', 'b', 'c'], current_index: 2 });
    expect(rebuildOnFirstWorkingRungReportingCurrent(s, m, neverReady, null).rung).toBe(
      'play_order',
    );
    const state = useQueueStore.getState();
    expect(orderedIds()).toEqual(['a', 'c']);
    expect(state.currentTrack()?.title).toBe('Track c');
    expect(state.shuffled).toBe(false);
  });

  it('marks the queue shuffled when the saved state was shuffled', () => {
    const m = mapOf(track('a'), track('b'));
    const s = saved({ track_ids: ['b', 'a'], shuffled: true });
    expect(rebuildOnFirstWorkingRungReportingCurrent(s, m, neverReady, null).rung).toBe(
      'play_order',
    );
    expect(useQueueStore.getState().shuffled).toBe(true);
  });
});

describe('rebuildOnFirstWorkingRungReportingCurrent', () => {
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

    expect(rebuildOnFirstWorkingRungReportingCurrent(s, m, isReadyIn(m), null).rung).toBe(
      'natural',
    );

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

    expect(rebuildOnFirstWorkingRungReportingCurrent(s, m, isReadyIn(m), null).rung).toBe(
      'play_order',
    );

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

    expect(rebuildOnFirstWorkingRungReportingCurrent(s, m, isReadyIn(m), null).rung).toBe(
      'exhausted',
    );

    expect(useQueueStore.getState().tracks).toHaveLength(0);
    expect(reportedTally()).toMatchObject({
      queue_rebuild_natural: 0,
      queue_rebuild_play_order: 0,
      queue_rebuild_exhausted: 1,
    });
  });
});
