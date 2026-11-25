import { useCallback, useRef, useState, type MutableRefObject } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { DiscoveryResult, DiscoveryKind, FeaturedArtist } from '@shared/catalog';
import { useNavigator, type Navigator } from '@shared/navigation';
import {
  openDetail,
  type DetailRoute,
  featuringHref,
  unreleasedHref,
} from '@features/detail/core/detailNavigation';
import type { UnreleasedWhere } from '@features/detail/core/unreleasedView';
import { resolveEntityQuery } from '@features/detail/core/resolveEntityQuery';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';
import { useProvidedNavigator } from '@features/detail/view/DetailNavigatorProvider';

export function useDetailNavigator(): Navigator {
  const appNavigator = useNavigator();
  return useProvidedNavigator() ?? appNavigator;
}

export function useOpenDetail(detailRoute: DetailRoute): (picked: DiscoveryResult) => void {
  const navigator = useDetailNavigator();
  return (picked) => openDetail(navigator, detailRoute, picked);
}

export function useOpenFeaturing(detailRoute: DetailRoute): (artist: FeaturedArtist) => void {
  const navigator = useDetailNavigator();
  return (artist) => navigator.push(featuringHref(detailRoute, artist));
}

export function useOpenUnreleased(
  detailRoute: DetailRoute,
): (artist: DiscoveryResult, where: UnreleasedWhere) => void {
  const navigator = useDetailNavigator();
  return (artist, where) => navigator.push(unreleasedHref(detailRoute, artist, where));
}

type LateralNavState = 'idle' | 'searching';

export type LateralNavHandle = {
  navigateTo: (query: string, kind: DiscoveryKind) => Promise<void>;
  state: LateralNavState;
  error: string | null;
};

type View = { state: LateralNavState; error: string | null };
type Outcome = { picked: DiscoveryResult } | { message: string };
type Load = () => Promise<DiscoveryResult[]>;
type Task = () => Promise<Outcome>;
type OnPicked = (picked: DiscoveryResult) => void;
type Gate = {
  busy: MutableRefObject<boolean>;
  setView: (view: View) => void;
};

const IDLE: View = { state: 'idle', error: null };
const SEARCHING: View = { state: 'searching', error: null };
const SEARCH_FAILED_MESSAGE = "Couldn't search, try again";

function notFoundMessage(kind: DiscoveryKind, query: string): string {
  const kindLabel = kind === 'artist' ? 'Artist' : 'Album';
  return `${kindLabel} not found: "${query}"`;
}

type Search = { query: string; kind: DiscoveryKind };

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function warnFetchFailed(port: DetailPort, { query, kind }: Search, error: unknown): void {
  port.warn('[detail] lateral nav fetch failed', { query, kind, error: errorMessage(error) });
}

function fetchResolved(
  qc: QueryClient,
  port: DetailPort,
  query: string,
  kind: DiscoveryKind,
): Promise<DiscoveryResult[]> {
  return qc.fetchQuery({ ...resolveEntityQuery(kind, query, 1, port), retry: false });
}

function outcomeOf(results: DiscoveryResult[], { query, kind }: Search): Outcome {
  const picked = results[0];
  return picked === undefined ? { message: notFoundMessage(kind, query) } : { picked };
}

async function lookup(port: DetailPort, load: Load, search: Search): Promise<Outcome> {
  try {
    return outcomeOf(await load(), search);
  } catch (error) {
    warnFetchFailed(port, search, error);
    return { message: SEARCH_FAILED_MESSAGE };
  }
}

async function runSearch(gate: Gate, task: Task, onPicked: OnPicked): Promise<void> {
  if (gate.busy.current) return;
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
  const busy = useRef(false);
  const run = useCallback(
    (task: Task, onPicked: OnPicked) => runSearch({ busy, setView }, task, onPicked),
    [],
  );
  return [view, run];
}

export function useLateralNav(detailRoute: DetailRoute): LateralNavHandle {
  const openDetail = useOpenDetail(detailRoute);
  const queryClient = useQueryClient();
  const port = useDetailPort();
  const [{ state, error }, run] = useSearchSession();
  const navigateTo = (query: string, kind: DiscoveryKind) =>
    run(
      () => lookup(port, () => fetchResolved(queryClient, port, query, kind), { query, kind }),
      openDetail,
    );
  return { navigateTo, state, error };
}
