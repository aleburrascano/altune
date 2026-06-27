/**
 * detail-handoff — module-level holder for the last-tapped discovery result.
 *
 * The seam between the discover and detail features: Discover sets the tapped
 * result here and navigates; DetailScreen reads it on mount. Lives in shared/
 * because it has two real consumers (discover writes, detail reads), so it
 * cannot be a cross-feature import. The detail screen is fed by this in-memory
 * handoff, not a per-item backend fetch (spec Design Considerations). A cold
 * start (deep link, reload) leaves this null, and DetailScreen redirects to
 * /discover.
 */

import type { DiscoveryResult } from '../api-client/discovery';

let _lastTapped: DiscoveryResult | null = null;
// The search_id of the search the tapped result came from, if any. Threaded so
// downstream engagement events (library_add, play) can join back to the search.
let _searchId: string | null = null;

export function setDetailHandoff(result: DiscoveryResult, searchId?: string): void {
  _lastTapped = result;
  _searchId = searchId ?? null;
}

export function getDetailHandoff(): DiscoveryResult | null {
  return _lastTapped;
}

// getDetailHandoffSearchId returns the originating search_id for the handed-off
// result, or null when the result did not come from a search.
export function getDetailHandoffSearchId(): string | null {
  return _searchId;
}

export function clearDetailHandoff(): void {
  _lastTapped = null;
  _searchId = null;
}
