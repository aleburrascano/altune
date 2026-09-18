import { useRef, useState } from 'react';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { enqueueCritical } from '@shared/telemetry/outbox';

import { trackExtras } from '../extras-accessors';
import { useDetailHandoff } from '../handoff-context';

export function useReportWrongAlbum(result: DiscoveryResult): {
  report: () => void;
  reported: boolean;
} {
  const [reported, setReported] = useState(false);
  const reportedRef = useRef(false);
  const searchId = useDetailHandoff()?.searchId;

  const report = (): void => {
    if (reportedRef.current) return;
    reportedRef.current = true;
    setReported(true);
    const album = trackExtras(result.extras).album;
    void enqueueCritical({
      type: 'wrong_album',
      search_id: searchId ?? undefined,
      payload: {
        kind: result.kind,
        title: result.title,
        subtitle: result.subtitle ?? null,
        album,
        ...(result.result_signature != null
          ? { result_signature: result.result_signature }
          : {}),
      },
    });
  };

  return { report, reported };
}
