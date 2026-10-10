import { expectType, type IsAssignable, type Not } from '@testing/typeAssertions';

import type { PlaylistResponse } from '@shared/apiClient/playlistResponse';
import { ACQUISITION_STATUSES, type TrackResponse } from '@shared/apiClient/trackResponse';

describe('trackResponse', () => {
  it('refuses a PlaylistResponse where a TrackResponse belongs', () => {
    expectType<Not<IsAssignable<PlaylistResponse, TrackResponse>>>();
  });

  it('lists the acquisition statuses', () => {
    expect(ACQUISITION_STATUSES).toEqual(['pending', 'ready', 'failed']);
  });
});
