import { asTrackId } from '@shared/api-client/ids';

import { activeMutationId } from '../activeMutationId';

const TRACK = asTrackId('track-1');
const OTHER_TRACK = asTrackId('track-2');

function mutation(isPending: boolean, variables: ReturnType<typeof asTrackId> | undefined) {
  return { isPending, variables };
}

describe('activeMutationId — the id a mutation is acting on right now', () => {
  it('is the id the pending run was started with', () => {
    expect(activeMutationId(mutation(true, TRACK))).toBe(TRACK);
  });

  it('is undefined once the run settles, even though the last variables linger', () => {
    expect(activeMutationId(mutation(false, TRACK))).toBeUndefined();
  });

  it('is undefined for a mutation that has never run', () => {
    expect(activeMutationId(mutation(false, undefined))).toBeUndefined();
  });

  it('matches only the track the pending run names', () => {
    const pendingOnTrack = mutation(true, TRACK);

    expect(activeMutationId(pendingOnTrack) === TRACK).toBe(true);
    expect(activeMutationId(pendingOnTrack) === OTHER_TRACK).toBe(false);
  });
});
