import { useEffect, useRef } from 'react';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';
import { setTrackNumber } from '@shared/api-client/tracks';

import { trackExtras } from '../extras-accessors';

export function usePersistTrackNumbers(
  localTracks: readonly TrackResponse[],
  positioned: readonly DiscoveryResult[],
): void {
  const done = useRef<Set<string>>(new Set());

  useEffect(() => {
    const posById = new Map<string, number>();
    for (const t of positioned) {
      const te = trackExtras(t.extras);
      if (te.trackId != null && te.trackPosition != null) {
        posById.set(te.trackId, te.trackPosition);
      }
    }
    for (const lt of localTracks) {
      if (lt.track_number != null || lt.id.startsWith('optimistic:') || done.current.has(lt.id)) {
        continue;
      }
      const pos = posById.get(lt.id);
      if (pos == null) {
        continue;
      }
      done.current.add(lt.id);
      void setTrackNumber(lt.id, pos).catch(() => done.current.delete(lt.id));
    }
  }, [localTracks, positioned]);
}
