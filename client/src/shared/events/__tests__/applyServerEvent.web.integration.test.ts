import { QueryClient } from '@tanstack/react-query';

import { asTrackId } from '@shared/apiClient';
import { applyServerEvent } from '@shared/events';
import { usePinnedStore } from '@shared/offline';

describe('track_acquisition_completed on web', () => {
  it('leaves a pinned download as it was, since web keeps no offline copies', () => {
    const entry = { trackId: asTrackId('t1'), status: 'ready' as const, uri: 'file:///stale.mp3' };
    usePinnedStore.setState({ entries: { t1: entry }, queue: [], isWorking: false });

    applyServerEvent(new QueryClient(), {
      id: '1',
      type: 'track_acquisition_completed',
      data: { track_id: 't1', audio_ref: 'ref-1' },
    });

    expect(usePinnedStore.getState().entries.t1).toEqual(entry);
  });
});
