import type { BackfillFeaturedResult as WireBackfillResult } from '@shared/apiClient';

import type { BackfillFeaturedResult } from '@features/settings/core/backfillResult';

export function toBackfillFeaturedResult(wire: WireBackfillResult): BackfillFeaturedResult {
  return {
    scanned: wire.scanned,
    updated: wire.updated,
    truncated: wire.truncated,
    nextOffset: wire.nextOffset,
  };
}
