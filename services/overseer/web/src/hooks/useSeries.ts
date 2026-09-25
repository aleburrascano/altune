import { useCallback, useContext, useState } from "react";

import { fetchSeries, TokensContext } from "../api";
import type { Range, SeriesPoint, SeriesResponse } from "../types";
import { usePolledFetch } from "./usePolledFetch";

export type SeriesMap = Record<string, SeriesPoint[]>;

export type SeriesState = { status: "idle" | "ready" | "unavailable"; series: SeriesMap; refetchFailed: boolean };

const DEFAULT_POLL_MS = 30_000;

const IDLE: SeriesState = { status: "idle", series: {}, refetchFailed: false };

export function useSeries(id: string, range: Range, pollMs: number = DEFAULT_POLL_MS): SeriesState {
  const tokens = useContext(TokensContext);
  const [state, setState] = useState<SeriesState>(IDLE);

  const load = useCallback(() => fetchSeries(tokens!, id, range), [tokens, id, range]);
  const onResult = useCallback(
    (res: SeriesResponse) => setState({ status: "ready", series: res.series, refetchFailed: false }),
    [],
  );
  const onError = useCallback(
    () =>
      setState((prev) =>
        prev.status === "ready"
          ? { ...prev, refetchFailed: true }
          : { status: "unavailable", series: {}, refetchFailed: false },
      ),
    [],
  );

  usePolledFetch(tokens !== null, load, onResult, onError, pollMs);

  return state;
}
