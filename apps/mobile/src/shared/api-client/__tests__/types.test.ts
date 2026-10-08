import { expectType, type IsAssignable, type Not } from '@testing/typeAssertions';

import type {
  PlaylistDetailResponse as NewPlaylistDetailResponse,
  PlaylistResponse,
} from '@shared/api-client/playlistResponse';
import {
  ACQUISITION_STATUSES,
  type TrackResponse as NewTrackResponse,
} from '@shared/api-client/trackResponse';
import type {
  PlaylistDetailResponse as ShimPlaylistDetailResponse,
  TrackResponse as ShimTrackResponse,
} from '@shared/api-client/types';

describe('api-client types shim', () => {
  it('keeps TrackResponse assignable both ways', () => {
    expectType<IsAssignable<ShimTrackResponse, NewTrackResponse>>();
    expectType<IsAssignable<NewTrackResponse, ShimTrackResponse>>();
  });

  it('keeps PlaylistDetailResponse assignable both ways', () => {
    expectType<IsAssignable<ShimPlaylistDetailResponse, NewPlaylistDetailResponse>>();
    expectType<IsAssignable<NewPlaylistDetailResponse, ShimPlaylistDetailResponse>>();
  });

  it('refuses a PlaylistResponse where a TrackResponse belongs', () => {
    expectType<Not<IsAssignable<PlaylistResponse, ShimTrackResponse>>>();
  });

  it('lists the acquisition statuses', () => {
    expect(ACQUISITION_STATUSES).toEqual(['pending', 'ready', 'failed']);
  });

  it('emits no runtime export', () => {
    expect(Object.keys(jest.requireActual('@shared/api-client/types'))).toEqual([]);
  });
});
