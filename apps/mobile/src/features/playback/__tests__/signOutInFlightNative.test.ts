// #827: an append, insert-next or upcoming reorder resolves signed URLs before it takes
// the native queue lock. One still in flight when the user signs out must not re-add the
// previous user's tracks to the native queue after the sign-out reset.

import { asTrackId } from '@shared/api-client/ids';

import {
  appendNativeTrack,
  insertNativeTrackNext,
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
