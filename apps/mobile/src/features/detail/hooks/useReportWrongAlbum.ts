/**
 * useReportWrongAlbum — emit the wrong_album hard-negative label.
 *
 * wrong_album is the one event type that already existed in the vocabulary with
 * nothing emitting it. A user reporting that the album attributed to a track is
 * wrong is a high-value relevance signal: a free hard negative for the
 * self-growing eval corpus. Fire-and-forget, once per mount, with the result
 * identity + originating search_id + result_signature so the corpus can join it.
 */

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
    // Label-critical: routed through the outbox (idempotency key + retry), not
    // fire-and-forget — a lost wrong_album is a lost hard negative.
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
