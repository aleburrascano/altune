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

/**
 * Rank of `result` in the flat `results[]` list, or -1 if it is not there.
 *
 * The blended view renders `top_result` and `sections[].items`, which are parsed as separate
 * objects from `results[]`, so a reference lookup alone misses every blended tap. Fall back to
 * the primary source identity, then the server's result signature.
 */
function globalRank(results: readonly DiscoveryResult[], result: DiscoveryResult): number {
  const byRef = results.indexOf(result);
  if (byRef >= 0) return byRef;
  const bySource = results.findIndex((r) => sameSource(r, result));
  if (bySource >= 0) return bySource;
  const signature = result.result_signature;
  return signature ? results.findIndex((r) => r.result_signature === signature) : -1;
}

/**
 * Records a `result_clicked` event for the tapped result, then opens its detail screen.
 *
 * Only the first tap per visit navigates: RN fires `onPress` for every touch-up, so a fast
 * double-tap (or two rows in quick succession) would otherwise push two detail screens and
 * overwrite the shared handoff the first one reads. The lock clears when the screen refocuses.
 */
export function useResultTap(
  searchData: DiscoverySearchResponse | undefined,
  committedQuery: string,
): ResultTapHandler {
  const router = useRouter();
  const recordEvent = useRecordEvent();
  const navigationPending = useRef(false);
  useFocusEffect(
    useCallback(() => {
      navigationPending.current = false;
    }, []),
  );
  return (result, position) => {
    if (navigationPending.current) return;
    navigationPending.current = true;
    Keyboard.dismiss();
    const globalIndex = searchData ? globalRank(searchData.results, result) : -1;
    recordEvent.mutate({
      type: 'result_clicked',
      query_norm: searchData?.query_norm ?? committedQuery,
      search_id: searchData?.search_id,
      payload: {
        kind: result.kind,
        title: result.title,
        subtitle: result.subtitle ?? null,
        position: globalIndex >= 0 ? globalIndex : position,
        confidence: result.confidence,
        provider: result.sources[0]?.provider ?? null,
        ...(result.result_signature != null ? { result_signature: result.result_signature } : {}),
      },
    });
    router.push(stashHandoffForDetail(result, searchData?.search_id));
  };
}
