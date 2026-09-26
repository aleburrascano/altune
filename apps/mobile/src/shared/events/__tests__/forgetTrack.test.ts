import { QueryClient, type InfiniteData } from '@tanstack/react-query';

import { asTrackId } from '@shared/api-client/ids';
import { toReady, toTrackStatus } from '@shared/api-client/trackAcquisition';
import type { ListTracksResponse } from '@shared/api-client/types';
import { patchTrackStatus, useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { libraryKeys } from '@shared/lib/query-keys';

import { forgetTrack } from '../forgetTrack';

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

describe('forgetTrack', () => {
  it('clears the track from the query caches and the status store', () => {
    const client = new QueryClient();
    const id = asTrackId('target');
    client.setQueryData<ListTracksResponse>(libraryKeys.lookup('q'), {
      items: [{ id }],
      total: 1,
    } as unknown as ListTracksResponse);
    client.setQueryData<InfiniteData<ListTracksResponse>>(libraryKeys.tracks('q', 'sort'), {
      pages: [{ items: [{ id }], total: 1 } as unknown as ListTracksResponse],
      pageParams: [0],
    });
    patchTrackStatus(id, toTrackStatus(toReady()));

    forgetTrack(client, id);

    expect(client.getQueryData<ListTracksResponse>(libraryKeys.lookup('q'))!.items).toEqual([]);
    expect(useTrackStatusStore.getState().statuses[id]).toBeUndefined();
  });
});
