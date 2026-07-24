import { useState } from 'react';

import { getDetailHandoffSearchId } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { enqueueCritical } from '@shared/telemetry/outbox';

export function useReportWrongAlbum(result: DiscoveryResult): {
  report: () => void;
  reported: boolean;
} {
  const [reported, setReported] = useState(false);

  const report = (): void => {
    if (reported) return;
    setReported(true);
    const album = typeof result.extras.album === 'string' ? result.extras.album : null;
    void enqueueCritical({
      type: 'wrong_album',
      search_id: getDetailHandoffSearchId() ?? undefined,
      payload: {
        kind: result.kind,
        title: result.title,
        subtitle: result.subtitle ?? null,
        album,
        result_signature: result.result_signature ?? null,
      },
    });
  };

  return { report, reported };
}
