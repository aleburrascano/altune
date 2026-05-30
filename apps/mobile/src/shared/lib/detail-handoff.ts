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

export function setDetailHandoff(result: DiscoveryResult): void {
  _lastTapped = result;
}

export function getDetailHandoff(): DiscoveryResult | null {
  return _lastTapped;
}

export function clearDetailHandoff(): void {
  _lastTapped = null;
}
