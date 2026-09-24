import { useContext, useEffect, useState } from "react";

import { fetchSeries, TokensContext } from "../api";
import type { Range, SeriesPoint } from "../types";

export type SeriesMap = Record<string, SeriesPoint[]>;

export type SeriesState = { status: "idle" | "ready" | "unavailable"; series: SeriesMap };

const DEFAULT_POLL_MS = 30_000;

const IDLE: SeriesState = { status: "idle", series: {} };

export function useSeries(id: string, range: Range, pollMs: number = DEFAULT_POLL_MS): SeriesState {
  const tokens = useContext(TokensContext);
  const [state, setState] = useState<SeriesState>(IDLE);

  useEffect(() => {
    if (!tokens) return;
    let active = true;
    const load = () =>
      fetchSeries(tokens, id, range).then(
        (res) => {
          if (active) setState({ status: "ready", series: res.series });
        },
        () => {
          if (active) setState((prev) => (prev.status === "ready" ? prev : { status: "unavailable", series: {} }));
        },
      );
    void load();
    const timer = setInterval(load, pollMs);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [tokens, id, range, pollMs]);

  return state;
}
