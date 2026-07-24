import type { DiscoveryResult } from '../api-client/discovery';

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
