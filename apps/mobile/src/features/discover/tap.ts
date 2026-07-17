/**
 * Tap → detail navigation seam for the discover feature.
 *
 * Pure-ish helper (matches the state.ts testing pattern): stashes the tapped
 * result into the shared in-memory handoff and returns the detail route for the
 * caller to push. Kept separate from DiscoverScreen.tsx so it is unit-testable
 * without rendering the full screen (React Query hooks + expo-image). Click
 * tracking is the caller's concern and stays fire-and-forget.
 */

import { setDetailHandoff } from '@shared/lib/detail-handoff';

import type { DiscoveryResult } from '@shared/api-client/discovery';

export function stashHandoffForDetail(
  result: DiscoveryResult,
  searchId?: string,
): '/discover/detail' {
  setDetailHandoff(result, searchId);
  return '/discover/detail';
}
