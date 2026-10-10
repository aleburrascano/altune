import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import type { Favorite, FavoritesResponse, FavoriteTarget } from '@shared/api-client/favorites';
import { useFavoritesPort } from '@features/discovery/ports/FavoritesPortProvider';
import type { FavoritesPort } from '@features/discovery/ports/favoritesPort';
import { RETRY_TAIL } from '@shared/failure/describeError';
import { discoveryKeys } from '@shared/query/queryKeys';
import { useAppMutation } from '@shared/query/useAppMutation';
import { guardedMutationOptions } from '@shared/session/guardedMutation';
import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';
import { showFailureAlert } from '@shared/ui';

const ACTION = 'favorites.toggle';
const FAILURE_MESSAGE = `Could not update your favorites. ${RETRY_TAIL}`;

type FavoritesApi = {
  isFavorite: (target: FavoriteTarget) => boolean;
  toggle: (target: FavoriteTarget) => void;
};

type SendRequest = { target: FavoriteTarget; favorite: boolean };

type FavoriteLane = {
  epoch: number;
  confirmed: boolean | undefined;
  desired: boolean;
  taps: number;
};

type LaneRun = {
  queryClient: QueryClient;
  target: FavoriteTarget;
  lane: FavoriteLane;
  send: (request: SendRequest) => Promise<void>;
};

const lanesByClient = new WeakMap<QueryClient, Map<string, FavoriteLane>>();

function lanesOf(queryClient: QueryClient): Map<string, FavoriteLane> {
  const existing = lanesByClient.get(queryClient);
  if (existing) return existing;
  const created = new Map<string, FavoriteLane>();
  lanesByClient.set(queryClient, created);
  return created;
}

function entryKey(kind: string, key: string): string {
  return `${kind}|${key}`;
}

function targetKey(target: FavoriteTarget): string {
  return entryKey(target.kind, target.favorite_key);
}

function isCached(current: FavoritesResponse | undefined, target: FavoriteTarget): boolean {
  return (
    current !== undefined &&
    current.items.some((f) => entryKey(f.kind, f.key) === targetKey(target))
  );
}

export function useFavorites(): FavoritesApi {
  const queryClient = useQueryClient();
  const port = useFavoritesPort();
  const { data } = useQuery({
    queryKey: discoveryKeys.favorites,
    queryFn: () => port.listFavorites(),
    staleTime: Infinity,
  });

  const { mutateAsync } = useAppMutation({
    action: ACTION,
    ...guardedMutationOptions({
      mutationFn: (request: SendRequest) => sendFavorite(port, request),
    }),
  });

  const saved = new Set((data?.items ?? []).map((f) => entryKey(f.kind, f.key)));

  function isFavorite(target: FavoriteTarget): boolean {
    return saved.has(targetKey(target));
  }

  function toggle(target: FavoriteTarget): void {
    const cached = queryClient.getQueryData<FavoritesResponse>(discoveryKeys.favorites);
    const favorite = !isCached(cached, target);
    showFavorite(queryClient, target, favorite);
    const lane = joinLane(queryClient, target, favorite);
    if (lane) void drainLane({ queryClient, target, lane, send: mutateAsync });
  }

  return { isFavorite, toggle };
}

function joinLane(queryClient: QueryClient, target: FavoriteTarget, favorite: boolean) {
  const epoch = currentSessionEpoch();
  const lanes = lanesOf(queryClient);
  const running = lanes.get(targetKey(target));
  if (running?.epoch === epoch) return retarget(running, favorite);
  const lane: FavoriteLane = { epoch, confirmed: !favorite, desired: favorite, taps: 1 };
  lanes.set(targetKey(target), lane);
  return lane;
}

function retarget(running: FavoriteLane, favorite: boolean): undefined {
  running.desired = favorite;
  running.taps += 1;
  return undefined;
}

async function drainLane(run: LaneRun): Promise<void> {
  const { lane } = run;
  if (lane.desired === lane.confirmed || !isSameSession(lane.epoch)) {
    settleLane(run);
    return;
  }
  await sendLatest(run);
  await drainLane(run);
}

async function sendLatest(run: LaneRun): Promise<void> {
  const { target, lane, send } = run;
  const sent = { ...lane };
  try {
    await send({ target, favorite: sent.desired });
    lane.confirmed = sent.desired;
  } catch {
    afterFailure(run, sent);
  }
}

function afterFailure(run: LaneRun, sent: FavoriteLane): void {
  run.lane.confirmed = undefined;
  if (run.lane.taps === sent.taps) rollBack(run, sent.confirmed ?? !sent.desired);
}

function rollBack({ queryClient, target, lane }: LaneRun, lastKnown: boolean): void {
  lane.confirmed = lastKnown;
  lane.desired = lastKnown;
  if (!isSameSession(lane.epoch)) return;
  showFavorite(queryClient, target, lastKnown);
  showFailureAlert({ surface: ACTION, title: 'Update failed', message: FAILURE_MESSAGE });
}

function settleLane({ queryClient, target, lane }: LaneRun): void {
  const lanes = lanesOf(queryClient);
  if (lanes.get(targetKey(target)) === lane) lanes.delete(targetKey(target));
  if (lanes.size > 0 || !isSameSession(lane.epoch)) return;
  void queryClient.invalidateQueries({ queryKey: discoveryKeys.favorites });
}

async function sendFavorite(port: FavoritesPort, { target, favorite }: SendRequest): Promise<void> {
  const { kind, title, subtitle, image_url } = target;
  const send = favorite ? port.addFavorite : port.removeFavorite;
  await send({ kind, title, subtitle, image_url });
}

function showFavorite(queryClient: QueryClient, target: FavoriteTarget, favorite: boolean): void {
  queryClient.setQueryData<FavoritesResponse>(discoveryKeys.favorites, (current) =>
    withFavorite(current, target, favorite),
  );
}

function withFavorite(
  current: FavoritesResponse | undefined,
  target: FavoriteTarget,
  favorite: boolean,
): FavoritesResponse {
  const others = othersThan(current, target);
  const next = favorite ? [entryFor(target), ...others] : others;
  return { items: next, total: next.length };
}

function othersThan(current: FavoritesResponse | undefined, target: FavoriteTarget): Favorite[] {
  if (current === undefined) return [];
  return current.items.filter((f) => entryKey(f.kind, f.key) !== targetKey(target));
}

function entryFor(target: FavoriteTarget): Favorite {
  return {
    kind: target.kind,
    key: target.favorite_key,
    title: target.title,
    subtitle: target.subtitle,
    image_url: target.image_url,
  };
}
