// An append, insert-next or upcoming reorder resolves signed URLs before it takes the
// native queue lock. Anything that replaces the queue it was computed against while it is
// in flight must supersede it — a sign-out (#827) or a switch to another queue (#1731) —
// or it lands the old queue's tracks on the native player.

import { asTrackId } from '@shared/api-client/ids';
import { trackKey } from '@shared/playback/trackKey';

import {
  appendNativeTrack,
  insertNativeTrackNext,
  loadNativeQueue,
  reorderUpcomingNative,
} from '../loadNativeTrack';
import { resetPlaybackForSignOut } from '../service';

import { libraryTrack } from './fixtures';

const { __player } = jest.requireMock('react-native-track-player');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok-a' } }, error: null }),
    },
  },
}));

const A_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-of-a') } });
const B_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-of-b') } });

// Which tracks the native player was handed, in call order (a multi-track add counts as
// its tracks). A call count alone cannot say which queue an add came from.
function nativeAddedIds(): string[] {
  const addCalls = __player.calls('add') as [{ id: string } | { id: string }[]][];
  return addCalls.flatMap(([added]) => (Array.isArray(added) ? added : [added])).map((t) => t.id);
}

describe('native queue ops in flight at sign-out (#827)', () => {
  it('an append started before sign-out does not add the track after the reset', async () => {
    const append = appendNativeTrack(A_TRACK);
    await resetPlaybackForSignOut();
    await append;

    expect(__player.calls('reset')).toHaveLength(1);
    expect(__player.calls('add')).toHaveLength(0);
  });

  it('an insert-next started before sign-out does not add the track after the reset', async () => {
    const insert = insertNativeTrackNext(A_TRACK, 1);
    await resetPlaybackForSignOut();
    await insert;

    expect(__player.calls('add')).toHaveLength(0);
  });

  it('an upcoming reorder started before sign-out leaves the reset queue alone', async () => {
    const reorder = reorderUpcomingNative([A_TRACK]);
    await resetPlaybackForSignOut();
    await reorder;

    expect(__player.calls('removeUpcomingTracks')).toHaveLength(0);
    expect(__player.calls('add')).toHaveLength(0);
  });

  it('an append started after sign-out still reaches the native queue', async () => {
    await resetPlaybackForSignOut();
    await appendNativeTrack(A_TRACK);

    expect(__player.calls('add')).toHaveLength(1);
  });
});

// A full requeue — "Play Now" on another playlist, retry(), play(track) — claims a load
// token but bumps no session epoch, which was all these ops watched before #1731, so they
// sailed past the switch and mutated the queue that replaced the one they were built for.
describe('native queue ops in flight at a queue switch (#1731)', () => {
  const loadQueueB = () => loadNativeQueue([B_TRACK], 0, { autoplay: false });

  it('an append started against the old queue does not add its track to the new one', async () => {
    const append = appendNativeTrack(A_TRACK);
    await loadQueueB();
    await append;

    expect(nativeAddedIds()).toEqual([trackKey(B_TRACK)]);
  });

  it('an insert-next started against the old queue does not add its track to the new one', async () => {
    const insert = insertNativeTrackNext(A_TRACK, 1);
    await loadQueueB();
    await insert;

    expect(nativeAddedIds()).toEqual([trackKey(B_TRACK)]);
  });

  it('an upcoming reorder started against the old queue leaves the new one intact', async () => {
    const reorder = reorderUpcomingNative([A_TRACK]);
    await loadQueueB();
    await reorder;

    expect(__player.calls('removeUpcomingTracks')).toHaveLength(0);
    expect(nativeAddedIds()).toEqual([trackKey(B_TRACK)]);
  });

  it('an append started after the switch reaches the new queue', async () => {
    await loadQueueB();
    await appendNativeTrack(A_TRACK);

    expect(nativeAddedIds()).toEqual([trackKey(B_TRACK), trackKey(A_TRACK)]);
  });
});
