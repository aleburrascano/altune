import { renderHook } from '@testing-library/react-native';
import {
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
  useTrackStatusStore,
} from '@shared/acquisition';
import { asTrackId } from '@shared/catalog';

import { ownedTrack, useResolvedOwnedTrack } from '@features/detail';
import type { OwnedTrack, TrackIdentity } from '@features/detail';

const SAVED_ID = asTrackId('saved-1');
const IDENTITY: TrackIdentity = { title: 'Idioteque', artist: 'Radiohead' };

function resolve(stamped: OwnedTrack | null, identity?: TrackIdentity): OwnedTrack | null {
  return renderHook(() => useResolvedOwnedTrack(stamped, identity)).result.current;
}

beforeEach(() => {
  useTrackStatusStore.getState().reset();
});

afterEach(() => {
  useTrackStatusStore.getState().reset();
});

describe('useResolvedOwnedTrack', () => {
  it('finds a track saved under the same title and artist when the row carries no id', () => {
    linkTrackIdentity(trackIdentityKey(IDENTITY.title, 'Radiohead'), SAVED_ID);
    patchTrackStatus(SAVED_ID, { acquisitionStatus: 'pending', failureMessage: null });

    expect(resolve(null, IDENTITY)).toEqual({ trackId: SAVED_ID, acquisitionStatus: 'pending' });
  });

  it('finds nothing for a row without an artist, and does not throw', () => {
    linkTrackIdentity(trackIdentityKey(IDENTITY.title, 'Radiohead'), SAVED_ID);

    expect(resolve(null, { title: IDENTITY.title, artist: null })).toBeNull();
  });

  it('keeps the stamped track when no identity is given and nothing is live', () => {
    const stamped = ownedTrack(SAVED_ID, 'ready', null);

    expect(resolve(stamped)).toBe(stamped);
  });

  it('keeps a live failure the retry would not fix as not retryable', () => {
    patchTrackStatus(SAVED_ID, {
      acquisitionStatus: 'failed',
      failureMessage: 'duplicate',
      retryable: false,
    });

    expect(resolve(ownedTrack(SAVED_ID, 'pending', null))).toEqual({
      trackId: SAVED_ID,
      acquisitionStatus: 'failed',
      failureMessage: 'duplicate',
      retryable: false,
    });
  });

  it('treats a live failure with no retry hint as retryable', () => {
    patchTrackStatus(SAVED_ID, { acquisitionStatus: 'failed', failureMessage: 'timeout' });

    expect(resolve(ownedTrack(SAVED_ID, 'pending', null))).toEqual({
      trackId: SAVED_ID,
      acquisitionStatus: 'failed',
      failureMessage: 'timeout',
      retryable: true,
    });
  });
});
