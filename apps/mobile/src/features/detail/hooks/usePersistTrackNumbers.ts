import { useEffect, useRef } from 'react';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';
import { setTrackNumber } from '@shared/api-client/tracks';

import { trackExtras } from '../extras-accessors';

/**
 * Persist-as-you-browse: write back the album positions the detail screen derived
 * (`_withAlbumPositions`) for library tracks saved before track_number was
 * captured, so the database self-heals as you open albums.
 *
 * Fire-and-forget PATCH per track, deduped for the screen's lifetime by a ref;
 * the server side is fill-only, so this can never overwrite a real value. Only
 * acts when a track has a derived position AND its stored number is still null —
 * so on discovery albums (positions already stored) it does nothing.
 */
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
      // Skip tracks that already have a number, optimistic placeholders (no real
      // id yet), and ones already attempted this session.
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
