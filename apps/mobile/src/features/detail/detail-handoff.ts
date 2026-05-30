/**
 * detail-handoff — module-level holder for the last-tapped discovery result.
 *
 * The detail screen is fed by an in-memory handoff, not a per-item backend
 * fetch (spec Design Considerations): Discover sets the tapped result here and
 * navigates; DetailScreen reads it on mount. A cold start (deep link, reload)
 * leaves this null, and DetailScreen redirects to /discover.
 */

import type { DiscoveryResult } from '../../shared/api-client/discovery';

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
