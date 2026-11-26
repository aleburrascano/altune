import { useState } from 'react';

import type { Navigator } from '@shared/navigation';
import { useRecordEvent } from '@shared/telemetry';
import type { DiscoverEnv } from '@features/discovery/view/search/screenEnv';
import { stashHandoffForDetail } from '@features/discovery/core/stashHandoffForDetail';
import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/catalog';

export type ResultTapHandler = (tapped: DiscoveryResult, position: number) => void;

function sameSource(a: DiscoveryResult, b: DiscoveryResult): boolean {
  const sa = a.sources[0];
  const sb = b.sources[0];
  return (
    a.kind === b.kind &&
    sa !== undefined &&
    sb !== undefined &&
    sa.external_id !== '' &&
    sa.provider === sb.provider &&
    sa.external_id === sb.external_id
  );
}

function globalRank(results: readonly DiscoveryResult[], result: DiscoveryResult): number {
  const byRef = results.indexOf(result);
  if (byRef >= 0) return byRef;
  const bySource = results.findIndex((r) => sameSource(r, result));
  if (bySource >= 0) return bySource;
  const signature = result.result_signature;
  return signature ? results.findIndex((r) => r.result_signature === signature) : -1;
}

function resultClickedIdentity(tapped: DiscoveryResult) {
  return {
    kind: tapped.kind,
    title: tapped.title,
    subtitle: tapped.subtitle ?? null,
    confidence: tapped.confidence,
    provider: tapped.sources[0]?.provider ?? null,
    ...(tapped.result_signature != null ? { result_signature: tapped.result_signature } : {}),
  };
}

function resultClickedPayload(
  searchData: DiscoverySearchResponse | undefined,
  tapped: DiscoveryResult,
  position: number,
) {
  const globalIndex = searchData ? globalRank(searchData.results, tapped) : -1;
  return { ...resultClickedIdentity(tapped), position: globalIndex >= 0 ? globalIndex : position };
}

type TapGate = { tryLock: () => boolean; release: () => void };

function createTapGate(): TapGate {
  let locked = false;
  return {
    tryLock: () => {
      if (locked) return false;
      locked = true;
      return true;
    },
    release: () => {
      locked = false;
    },
  };
}

type ResultClickArgs = {
  recordEvent: ReturnType<typeof useRecordEvent>;
  searchData: DiscoverySearchResponse | undefined;
  tapped: DiscoveryResult;
  position: number;
};

function recordResultClicked({ recordEvent, searchData, tapped, position }: ResultClickArgs): void {
  recordEvent.mutate({
    type: 'result_clicked',
    search_id: searchData?.search_id,
    payload: resultClickedPayload(searchData, tapped, position),
  });
}

type TapDeps = {
  dismissKeyboard: () => void;
  gate: TapGate;
  recordEvent: ReturnType<typeof useRecordEvent>;
  router: Navigator;
  searchData: DiscoverySearchResponse | undefined;
};

function tapArgs(deps: TapDeps, tapped: DiscoveryResult, position: number): ResultClickArgs {
  return { recordEvent: deps.recordEvent, searchData: deps.searchData, tapped, position };
}

function handleResultTap(deps: TapDeps, tapped: DiscoveryResult, position: number): void {
  if (!deps.gate.tryLock()) return;
  deps.dismissKeyboard();
  recordResultClicked(tapArgs(deps, tapped, position));
  deps.router.push(stashHandoffForDetail(tapped, deps.searchData?.search_id));
}

export function useResultTap(
  searchData: DiscoverySearchResponse | undefined,
  { navigator: router, useFocus, dismissKeyboard }: DiscoverEnv,
): ResultTapHandler {
  const recordEvent = useRecordEvent();
  const [gate] = useState(createTapGate);
  useFocus(gate.release);
  return (tapped, position) =>
    handleResultTap({ dismissKeyboard, gate, recordEvent, router, searchData }, tapped, position);
}
