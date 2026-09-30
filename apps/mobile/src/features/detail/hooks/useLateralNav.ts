import { useCallback, useRef, useState, type MutableRefObject } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';

import type { DiscoveryKind, DiscoveryResult } from '@shared/api-client/discovery';

import { useOpenDetail, type DetailRoute } from '../navigation';
import { resolveEntityQuery } from '../resolve-entity-query';
import { useDetailFetchEnabled } from './detailFetchGate';

type LateralNavState = 'idle' | 'searching';

export type LateralNavHandle = {
  navigateTo: (query: string, kind: DiscoveryKind) => Promise<void>;
  state: LateralNavState;
  error: string | null;
};

type View = { state: LateralNavState; error: string | null };
type Outcome = { picked: DiscoveryResult } | { message: string };
type Task = () => Promise<Outcome>;
type OnPicked = (picked: DiscoveryResult) => void;
type Gate = {
  busy: MutableRefObject<boolean>;
  enabled: boolean;
  setView: (view: View) => void;
};

const IDLE: View = { state: 'idle', error: null };
const SEARCHING: View = { state: 'searching', error: null };
const SEARCH_FAILED_MESSAGE = "Couldn't search, try again";

function notFoundMessage(kind: DiscoveryKind, query: string): string {
  const kindLabel = kind === 'artist' ? 'Artist' : 'Album';
  return `${kindLabel} not found: "${query}"`;
}

function warnFetchFailed(query: string, kind: DiscoveryKind, error: unknown): void {
  console.warn('[detail] lateral nav fetch failed', {
    query,
    kind,
    error: error instanceof Error ? error.message : String(error),
  });
}

async function lookup(qc: QueryClient, query: string, kind: DiscoveryKind): Promise<Outcome> {
  try {
    const results = await qc.fetchQuery({ ...resolveEntityQuery(kind, query, 1), retry: false });
    const picked = results[0];
    return picked === undefined ? { message: notFoundMessage(kind, query) } : { picked };
  } catch (error) {
    warnFetchFailed(query, kind, error);
    return { message: SEARCH_FAILED_MESSAGE };
  }
}

async function runSearch(gate: Gate, task: Task, onPicked: OnPicked): Promise<void> {
  if (gate.busy.current) return;
  if (!gate.enabled) return gate.setView({ ...IDLE, error: 'Search is temporarily unavailable' });
  gate.busy.current = true;
  gate.setView(SEARCHING);
  const outcome = await task();
  gate.busy.current = false;
  if ('picked' in outcome) {
    gate.setView(IDLE);
    return onPicked(outcome.picked);
  }
  gate.setView({ state: 'idle', error: outcome.message });
}

function useSearchSession(): [View, (task: Task, onPicked: OnPicked) => Promise<void>] {
  const [view, setView] = useState<View>(IDLE);
  const enabled = useDetailFetchEnabled();
  const busy = useRef(false);
  const run = useCallback(
    (task: Task, onPicked: OnPicked) => runSearch({ busy, enabled, setView }, task, onPicked),
    [enabled],
  );
  return [view, run];
}

export function useLateralNav(detailRoute: DetailRoute): LateralNavHandle {
  const openDetail = useOpenDetail(detailRoute);
  const queryClient = useQueryClient();
  const [{ state, error }, run] = useSearchSession();
  const navigateTo = (query: string, kind: DiscoveryKind) =>
    run(() => lookup(queryClient, query, kind), openDetail);
  return { navigateTo, state, error };
}
