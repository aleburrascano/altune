import { useCallback, useRef } from 'react';
import { useFocusEffect, useRouter } from 'expo-router';
import { Keyboard } from 'react-native';

import { useRecordEvent } from '@shared/telemetry/useRecordEvent';
import { stashHandoffForDetail } from '../handoff';
import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';

type ResultTapHandler = (result: DiscoveryResult, position: number) => void;

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

function useResetNavigationPendingOnFocus(navigationPendingRef: { current: boolean }): void {
  useFocusEffect(
    useCallback(() => {
      navigationPendingRef.current = false;
    }, [navigationPendingRef]),
  );
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
  navigationPendingRef: { current: boolean };
  recordEvent: ReturnType<typeof useRecordEvent>;
  router: ReturnType<typeof useRouter>;
  searchData: DiscoverySearchResponse | undefined;
};

function tapArgs(deps: TapDeps, tapped: DiscoveryResult, position: number): ResultClickArgs {
  return { recordEvent: deps.recordEvent, searchData: deps.searchData, tapped, position };
}

function handleResultTap(deps: TapDeps, tapped: DiscoveryResult, position: number): void {
  if (deps.navigationPendingRef.current) return;
  deps.navigationPendingRef.current = true;
  Keyboard.dismiss();
  recordResultClicked(tapArgs(deps, tapped, position));
  deps.router.push(stashHandoffForDetail(tapped, deps.searchData?.search_id));
}

export function useResultTap(searchData: DiscoverySearchResponse | undefined): ResultTapHandler {
  const router = useRouter();
  const recordEvent = useRecordEvent();
  const navigationPendingRef = useRef(false);
  useResetNavigationPendingOnFocus(navigationPendingRef);
  return (tapped, position) =>
    handleResultTap({ navigationPendingRef, recordEvent, router, searchData }, tapped, position);
}
