import { useCallback, useRef } from 'react';
import { useFocusEffect, useRouter } from 'expo-router';
import { Keyboard } from 'react-native';

import { useRecordEvent } from '@shared/telemetry/useRecordEvent';
import { stashHandoffForDetail } from '../handoff';
import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';

type ResultTapHandler = (result: DiscoveryResult, position: number) => void;

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
    const globalIndex = searchData?.results.indexOf(result) ?? -1;
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
