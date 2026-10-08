import { asTrackId } from '@shared/api-client/ids';

import {
  ascending,
  currentAfterMove,
  currentAfterRemoval,
  freshQueue,
  movedOrder,
  identityOrder,
  isPositionIn,
  reorderTail,
  resolveSyncTarget,
  withAppended,
  withoutTrack,
} from '@shared/playback/queueOrder';
import type { QueueState } from '@shared/playback/queueOrder';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

function track(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId(id) },
    title: `Track ${id}`,
    artist: 'Test Artist',
    artworkUrl: null,
  };
}

describe('identityOrder', () => {
  it('is empty for 0 and [0] for 1', () => {
    expect(identityOrder(0)).toEqual([]);
    expect(identityOrder(1)).toEqual([0]);
  });
});

describe('isPositionIn', () => {
  const LENGTH = 3;
  const LAST = LENGTH - 1;
  const FRACTIONAL = 1.5;

  it('accepts an in-range integer', () => {
    expect(isPositionIn(LENGTH, LAST)).toBe(true);
  });

  it.each([LENGTH, -1, FRACTIONAL])('rejects %s', (index) => {
    expect(isPositionIn(LENGTH, index)).toBe(false);
  });
});

describe('reorderTail', () => {
  const QUEUE_SIZE = 5;
  const QUEUED_NEXT = 3;
  const QUEUED_LAST = 1;
  const FIRST_REST = 2;
  const SECOND_REST = 4;

  it('puts up-next first, the rest, then appended, keeping the head', () => {
    const queued = { upNext: [QUEUED_NEXT], appended: [QUEUED_LAST] };
    const result = reorderTail(identityOrder(QUEUE_SIZE), 0, queued, ascending);
    expect(result).toEqual([0, QUEUED_NEXT, FIRST_REST, SECOND_REST, QUEUED_LAST]);
  });
});

describe('resolveSyncTarget', () => {
  const FIRST_HIT = 1;
  const CURSOR = 3;
  const SECOND_HIT = 4;
  const names = ['a', 'b', 'c', 'd', 'e', 'f'];
  names[FIRST_HIT] = 'dup';
  names[SECOND_HIT] = 'dup';
  const tracks = names.map((name) => track(name));
  const order = identityOrder(tracks.length);
  const dupKey = trackKey(track('dup'));

  it('returns index when the key matches the track there', () => {
    expect(resolveSyncTarget(tracks, order, FIRST_HIT, dupKey)).toBe(FIRST_HIT);
  });

  it('returns null for a look-alike key matching no track', () => {
    expect(resolveSyncTarget(tracks, order, FIRST_HIT, `${dupKey}x`)).toBeNull();
  });

  it('returns the nearest position when the key sits elsewhere', () => {
    expect(resolveSyncTarget(tracks, order, CURSOR, dupKey)).toBe(SECOND_HIT);
  });

  it('returns null with no key and an out-of-range index', () => {
    expect(resolveSyncTarget(tracks, order, tracks.length, undefined)).toBeNull();
    expect(resolveSyncTarget(tracks, order, -1, undefined)).toBeNull();
  });
});

describe('withoutTrack', () => {
  it('drops the removed index and shifts higher ones down', () => {
    const removed = 2;
    const above = removed + 1;
    expect(withoutTrack([0, removed, above], removed)).toEqual([0, removed]);
  });
});

describe('withAppended', () => {
  it('numbers new tracks from the current tracks length after growth', () => {
    const base: QueueState = {
      tracks: [track('a')],
      playOrder: [0],
      upNext: [],
      appended: [],
      currentIndex: 0,
      repeatMode: 'off',
      shuffled: false,
      source: null,
      resumePositionMs: 0,
      generation: 0,
    };
    const first = withAppended(base, [track('b')]);
    const grown: QueueState = { ...base, ...first, tracks: [...first.tracks!, track('c')] };
    const start = grown.tracks.length;
    const second = withAppended(grown, [track('d'), track('e')]);
    expect(second.appended).toEqual([...first.appended!, start, start + 1]);
    expect(second.playOrder).toEqual([0, ...first.appended!, start, start + 1]);
  });
});

const TWO = 2;
const THREE = 3;
const FOUR = 4;
const FIVE = 5;
const SIX = 6;

describe('currentAfterMove', () => {
  it('follows the current track when it is moved', () => {
    expect(currentAfterMove(TWO, TWO, 0)).toBe(0);
  });

  it('shifts down when a track before current moves to or past it', () => {
    expect(currentAfterMove(TWO, 0, THREE)).toBe(1);
  });

  it('shifts up when a track after current moves to or before it', () => {
    expect(currentAfterMove(TWO, FOUR, 1)).toBe(THREE);
  });

  it('stays put when the move is entirely after current', () => {
    expect(currentAfterMove(TWO, THREE, FOUR)).toBe(TWO);
  });
});

describe('movedOrder', () => {
  it('moves the first entry to the end', () => {
    expect(movedOrder([0, 1, TWO], 0, TWO)).toEqual([1, TWO, 0]);
  });

  it('returns a single-entry order unchanged', () => {
    expect(movedOrder([0], 0, 0)).toEqual([0]);
  });

  it('does not mutate its input', () => {
    const order = [0, 1, TWO];
    movedOrder(order, 0, TWO);
    expect(order).toEqual([0, 1, TWO]);
  });
});

describe('currentAfterRemoval', () => {
  it('is -1 when the only track is removed', () => {
    expect(currentAfterRemoval(0, 0, 0)).toBe(-1);
  });

  it('clamps when the last track, which is current, is removed', () => {
    expect(currentAfterRemoval(THREE, THREE, THREE)).toBe(TWO);
  });

  it('shifts down when a track before current is removed', () => {
    expect(currentAfterRemoval(THREE, 1, FOUR)).toBe(TWO);
  });

  it('stays put when a track after current is removed', () => {
    expect(currentAfterRemoval(1, TWO, FOUR)).toBe(1);
  });
});

describe('freshQueue', () => {
  const base = {
    tracks: [],
    playOrder: [],
    currentIndex: -1,
    shuffled: false,
    source: null,
  };

  it('bumps the generation and clears user-queued state', () => {
    const fresh = freshQueue({ ...base, generation: FOUR });
    expect(fresh.generation).toBe(FIVE);
    expect(fresh.upNext).toEqual([]);
    expect(fresh.appended).toEqual([]);
    expect(fresh.resumePositionMs).toBe(0);
  });

  it('increments from the generation passed in each time', () => {
    expect(freshQueue({ ...base, generation: FOUR }).generation).toBe(FIVE);
    expect(freshQueue({ ...base, generation: FOUR }).generation).toBe(FIVE);
    expect(freshQueue({ ...base, generation: FIVE }).generation).toBe(SIX);
  });
});
