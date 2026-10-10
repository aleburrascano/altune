import { asTrackId } from '@shared/apiClient/ids';
import type { QueueStateResponse } from '@shared/apiClient/playback';
import type { AcquisitionStatus, TrackResponse } from '@shared/apiClient/trackResponse';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { recordEvent } from '@shared/telemetry/recordEvent';

import {
  _resetPlaybackHealthForTest,
  flushPlaybackHealth,
} from '@features/playback/domain/playbackHealth';
import {
  currentOccurrence,
  currentTrackId,
  isSavedCurrentResumable,
  rebuildOnFirstWorkingRungReportingCurrent,
  reconstructPlayOrder,
  resolveResumeStartIndex,
  showSavedTrackWhileRehydrating,
} from '@features/playback/domain/queueResume';

describe('currentTrackId — resolving the saved cursor to an id', () => {
  it('returns the id at the saved index when it is in range', () => {
    expect(currentTrackId(['a', 'b', 'c'], 1)).toBe('b');
  });

  it('falls back to the first id when the saved index is out of range', () => {
    expect(currentTrackId(['a', 'b', 'c'], 9)).toBe('a');
  });

  it('falls back to the first id when the saved index is negative', () => {
    expect(currentTrackId(['a', 'b', 'c'], -1)).toBe('a');
  });

  it('returns the empty string when there are no saved ids', () => {
    expect(currentTrackId([], 0)).toBe('');
  });
});

describe('currentOccurrence — which copy of a duplicated id the saved cursor points at', () => {
  it('is 0 for the first copy', () => {
    expect(currentOccurrence(['a', 'b', 'a'], 0)).toBe(0);
  });

  it('counts the earlier copies of the same id', () => {
    expect(currentOccurrence(['a', 'b', 'a', 'a'], 3)).toBe(2);
  });

  it('is 0 when the saved index is out of range (the cursor falls back to the first id)', () => {
    expect(currentOccurrence(['a', 'b', 'a'], 7)).toBe(0);
    expect(currentOccurrence([], 0)).toBe(0);
  });
});

describe('resolveResumeStartIndex — where playback resumes against the currently valid ids', () => {
  it('is 0 when no valid ids remain', () => {
    expect(resolveResumeStartIndex(['a', 'b'], 1, [])).toBe(0);
  });

  it('follows the saved track by identity, not by its old position', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'c'], 2, ['x', 'c', 'y'])).toBe(1);
  });

  it('returns index 0 when the saved track is now the first valid id', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'c'], 2, ['c', 'x', 'y'])).toBe(0);
  });

  it('clamps to the saved index when no id was ever saved, not to the first valid track', () => {
    expect(resolveResumeStartIndex([], 2, ['x', 'y', 'z', 'w'])).toBe(2);
  });

  it('clamps to the last valid index when the saved track is gone and the index overshoots', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'c'], 2, ['x', 'y'])).toBe(1);
  });

  it('clamps a negative saved index to 0 when the saved track is gone', () => {
    expect(resolveResumeStartIndex(['a'], -3, ['x', 'y'])).toBe(0);
  });

  it('lands on the second copy of a duplicated track when the second copy was playing', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'a', 'c'], 2, ['a', 'b', 'a', 'c'])).toBe(2);
  });

  it('keeps the playing copy when an unavailable track before it was dropped', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'a', 'c'], 2, ['a', 'a', 'c'])).toBe(1);
  });

  it('falls back to the last copy when fewer copies remain than the saved occurrence', () => {
    expect(resolveResumeStartIndex(['a', 'a', 'a'], 2, ['x', 'a', 'y', 'a'])).toBe(3);
  });
});

describe('reconstructPlayOrder — rebuilding the play order over the currently present tracks', () => {
  it('maps play ids to their positions in the natural order', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'c'],
      ['c', 'a', 'b'],
      'a',
      0,
    );

    expect(playOrder).toEqual([2, 0, 1]);
    expect(currentIndex).toBe(1);
  });

  it('drops play ids that are no longer present in the natural order', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b'],
      ['a', 'ghost', 'b'],
      'b',
      0,
    );

    expect(playOrder).toEqual([0, 1]);
    expect(currentIndex).toBe(1);
  });

  it('keeps currentIndex at 0 when the current id is absent from the play ids', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'b'], ['b', 'a'], 'missing', 0);

    expect(playOrder).toEqual([1, 0]);
    expect(currentIndex).toBe(0);
  });

  it('lands on the second copy of a duplicated track when the second copy was playing', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'a', 'c'],
      ['a', 'b', 'a', 'c'],
      'a',
      1,
    );

    expect(playOrder).toEqual([0, 1, 2, 3]);
    expect(currentIndex).toBe(2);
  });

  it('stays on the first copy of a duplicated track when the first copy was playing', () => {
    const { currentIndex } = reconstructPlayOrder(['a', 'b', 'a'], ['a', 'b', 'a'], 'a', 0);

    expect(currentIndex).toBe(0);
  });

  it('maps each shuffled copy of a duplicated id to a distinct natural track', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'a'],
      ['a', 'a', 'b'],
      'a',
      1,
    );

    expect([...playOrder].sort()).toEqual([0, 1, 2]);
    expect(playOrder).toEqual([0, 2, 1]);
    expect(currentIndex).toBe(1);
  });

  it('falls back to the last copy when the play order holds fewer copies than the occurrence', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'b'], ['b', 'a'], 'a', 3);

    expect(playOrder).toEqual([1, 0]);
    expect(currentIndex).toBe(1);
  });

  it('reuses the last natural copy when the play order holds more copies than the natural order', () => {
    const { playOrder } = reconstructPlayOrder(['a', 'b'], ['a', 'b', 'a'], 'b', 0);

    expect(playOrder).toEqual([0, 1, 0]);
  });
});

describe('reconstructPlayOrder — reporting whether the saved current track was found', () => {
  it('reports found when the current id is among the play ids', () => {
    expect(reconstructPlayOrder(['a', 'b'], ['b', 'a'], 'a', 0).found).toBe(true);
  });

  it('reports found for the second copy of a duplicated track', () => {
    expect(reconstructPlayOrder(['a', 'a'], ['a', 'a'], 'a', 1).found).toBe(true);
  });

  it('reports not found when the current id is absent', () => {
    expect(reconstructPlayOrder(['a', 'b'], ['b', 'a'], 'missing', 0).found).toBe(false);
  });

  it('reports found for the first copy and not found when the id is gone', () => {
    expect(reconstructPlayOrder(['a', 'b'], ['a', 'b'], 'a', 1).found).toBe(true);
    expect(reconstructPlayOrder(['b'], ['b'], 'a', 0).found).toBe(false);
  });
});

describe('isSavedCurrentResumable — the resolveResumeStartIndex found flag', () => {
  it('is true when the saved current id is among the valid ids', () => {
    expect(isSavedCurrentResumable(['a', 'b', 'c'], 1, ['a', 'b'])).toBe(true);
  });

  it('is false when the saved current id was dropped, though an index is still clamped', () => {
    expect(isSavedCurrentResumable(['a', 'b', 'c'], 1, ['a', 'c'])).toBe(false);
    expect(resolveResumeStartIndex(['a', 'b', 'c'], 1, ['a', 'c'])).toBe(1);
  });

  it('is true for a kept duplicate occurrence', () => {
    expect(isSavedCurrentResumable(['a', 'b', 'a'], 2, ['a', 'a'])).toBe(true);
  });
});

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
