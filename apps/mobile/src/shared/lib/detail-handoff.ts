import type { DiscoveryResult } from '../api-client/discovery';
import { onSignOut } from '../auth/signOutCleanup';

let _lastTapped: DiscoveryResult | null = null;
let _searchId: string | null = null;

export function setDetailHandoff(result: DiscoveryResult, searchId?: string): void {
  _lastTapped = result;
  _searchId = searchId ?? null;
}

export function getDetailHandoff(): DiscoveryResult | null {
  return _lastTapped;
}

export function getDetailHandoffSearchId(): string | null {
  return _searchId;
}

export function clearDetailHandoff(): void {
  _lastTapped = null;
  _searchId = null;
}

// Process-lifetime state: without this, a detail screen opened before the next
// account taps anything would render the previous account's last-tapped result.
onSignOut(clearDetailHandoff);
