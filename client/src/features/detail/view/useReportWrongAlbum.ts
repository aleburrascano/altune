import { useRef, useState } from 'react';
import type { DiscoveryResult } from '@shared/catalog';
import { enqueueCritical } from '@shared/telemetry';

import { trackExtras } from '@features/detail/core/trackExtras';
import { useDetailHandoff } from '@features/detail/view/useDetailHandoff';

function wrongAlbumPayload(track: DiscoveryResult) {
  return {
    kind: track.kind,
    title: track.title,
    subtitle: track.subtitle ?? null,
    album: trackExtras(track.extras).album,
    ...(track.result_signature != null ? { result_signature: track.result_signature } : {}),
  };
}

function reportWrongAlbum(track: DiscoveryResult, searchId: string | null | undefined): void {
  void enqueueCritical({
    type: 'wrong_album',
    search_id: searchId ?? undefined,
    payload: wrongAlbumPayload(track),
  });
}

type UseReportWrongAlbumReturn = { report: () => void; reported: boolean };
type ReportOnceArgs = {
  track: DiscoveryResult;
  searchId: string | null | undefined;
  setReported: (v: boolean) => void;
  reportedRef: { current: boolean };
};

function reportOnce({ track, searchId, setReported, reportedRef }: ReportOnceArgs): void {
  if (reportedRef.current) return;
  reportedRef.current = true;
  setReported(true);
  reportWrongAlbum(track, searchId);
}

export function useReportWrongAlbum(track: DiscoveryResult): UseReportWrongAlbumReturn {
  const [reported, setReported] = useState(false);
  const reportedRef = useRef(false);
  const searchId = useDetailHandoff()?.searchId;
  const report = () => reportOnce({ track, searchId, setReported, reportedRef });
  return { report, reported };
}
