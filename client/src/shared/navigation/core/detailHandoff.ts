import type { DiscoveryResult } from '@shared/catalog';
import { onSignOut } from '@shared/session';

export type DetailHandoff = {
  readonly result: DiscoveryResult;
  readonly searchId: string | null;
};

export type DetailHref<P extends string> = {
  pathname: P;
  params: { handoff: string };
};

const MAX_HANDOFFS = 100;

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

export function detailHref<P extends string>(
  pathname: P,
  result: DiscoveryResult,
  searchId?: string,
): DetailHref<P> {
  return { pathname, params: { handoff: registerDetailHandoff(result, searchId) } };
}

const handoffsByAnyRouteParam: ReadonlyMap<unknown, DetailHandoff> = handoffs;

export function readDetailHandoff(id: string | string[] | undefined): DetailHandoff | null {
  return handoffsByAnyRouteParam.get(id) ?? null;
}

function clearDetailHandoffs(): void {
  handoffs.clear();
}

onSignOut(clearDetailHandoffs);
