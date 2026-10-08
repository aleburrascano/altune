import { asTrackId } from '@shared/api-client/ids';

import {
  ascending,
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
  it('accepts an in-range integer', () => {
    expect(isPositionIn(3, 2)).toBe(true);
  });

  it.each([3, -1, 1.5])('rejects %s', (index) => {
    expect(isPositionIn(3, index)).toBe(false);
  });
});

describe('reorderTail', () => {
  it('puts up-next first, the rest, then appended, keeping the head', () => {
    const result = reorderTail([0, 1, 2, 3, 4], 0, { upNext: [3], appended: [1] }, ascending);
    expect(result).toEqual([0, 3, 2, 4, 1]);
  });
});

describe('resolveSyncTarget', () => {
  const tracks = [track('a'), track('b'), track('c'), track('b'), track('b'), track('d')];
  const order = identityOrder(tracks.length);
  const keyB = trackKey(tracks[1]!);

  it('returns index when the key matches the track there', () => {
    expect(resolveSyncTarget(tracks, order, 1, keyB)).toBe(1);
  });

  it('returns null for a look-alike key matching no track', () => {
    expect(resolveSyncTarget(tracks, order, 1, `${keyB}x`)).toBeNull();
  });

  it('returns the nearest position when the key sits elsewhere', () => {
    expect(resolveSyncTarget(tracks, order, 3, keyB)).toBe(3);
    const moved = [track('a'), track('b'), track('c'), track('x'), track('b'), track('d')];
    expect(resolveSyncTarget(moved, order, 3, keyB)).toBe(4);
  });

  it('returns null with no key and an out-of-range index', () => {
    expect(resolveSyncTarget(tracks, order, 9, undefined)).toBeNull();
    expect(resolveSyncTarget(tracks, order, -1, undefined)).toBeNull();
  });
});

describe('withoutTrack', () => {
  it('drops the removed index and shifts higher ones down', () => {
    expect(withoutTrack([0, 2, 3], 2)).toEqual([0, 2]);
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
    const second = withAppended(grown, [track('d'), track('e')]);
    expect(second.appended).toEqual([1, 3, 4]);
    expect(second.playOrder).toEqual([0, 1, 3, 4]);
  });
});
