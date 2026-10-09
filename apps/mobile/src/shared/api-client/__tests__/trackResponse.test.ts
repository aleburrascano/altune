import { expectType, type IsAssignable, type Not } from '@testing/typeAssertions';

import type { PlaylistResponse } from '@shared/api-client/playlistResponse';
import { ACQUISITION_STATUSES, type TrackResponse } from '@shared/api-client/trackResponse';

describe('trackResponse', () => {
  it('refuses a PlaylistResponse where a TrackResponse belongs', () => {
    expectType<Not<IsAssignable<PlaylistResponse, TrackResponse>>>();
  });

  it('lists the acquisition statuses', () => {
    expect(ACQUISITION_STATUSES).toEqual(['pending', 'ready', 'failed']);
  });
});
