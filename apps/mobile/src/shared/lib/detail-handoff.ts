import type { DiscoveryResult } from '../api-client/discovery';
import { onSignOut } from '../session/signOutCleanup';

// The tapped DiscoveryResult travels to the detail screen as a route param that
// names a registry entry, not as the result itself: a DiscoveryResult (sources,
// extras) is too large to serialise into a URL, and keying each navigation by
// its own id means a second tap can never overwrite what an earlier detail
// screen reads.

export type DetailHandoff = {
  readonly result: DiscoveryResult;
  readonly searchId: string | null;
};

export type DetailHref<P extends string> = {
  pathname: P;
  params: { handoff: string };
};

// Bounds memory across a long session; far deeper than any real detail stack.
const MAX_HANDOFFS = 100;

// Per-process prefix so an id from a previous app/web session (browser history)
// can never resolve to an entry minted in this one.
const SESSION_PREFIX = Math.random().toString(36).slice(2, 10);

const handoffs = new Map<string, DetailHandoff>();
let nextSeq = 0;

function registerDetailHandoff(result: DiscoveryResult, searchId?: string): string {
  nextSeq += 1;
  const id = `${SESSION_PREFIX}-${nextSeq}`;
  handoffs.set(id, { result, searchId: searchId ?? null });
  for (const oldest of handoffs.keys()) {
    if (handoffs.size <= MAX_HANDOFFS) break;
    handoffs.delete(oldest);
  }
  return id;
}

// Builds the href that carries `result` to a detail route. Push the returned
// href; the detail screen resolves it with readDetailHandoff.
export function detailHref<P extends string>(
  pathname: P,
  result: DiscoveryResult,
  searchId?: string,
): DetailHref<P> {
  return { pathname, params: { handoff: registerDetailHandoff(result, searchId) } };
}

export function readDetailHandoff(id: string | string[] | undefined): DetailHandoff | null {
  if (typeof id !== 'string') return null;
  return handoffs.get(id) ?? null;
}

export function clearDetailHandoffs(): void {
  handoffs.clear();
}

// Process-lifetime state: without this, a detail screen opened before the next
// account taps anything could resolve a handoff minted for the previous account.
onSignOut(clearDetailHandoffs);
