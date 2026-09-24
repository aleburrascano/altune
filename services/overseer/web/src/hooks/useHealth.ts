import { useCallback, useContext, useState } from "react";

import { fetchHealth, TokensContext } from "../api";
import type { OverseerHealth } from "../types";
import { usePolledFetch } from "./usePolledFetch";

const DEFAULT_POLL_MS = 15_000;

export function useHealth(pollMs: number = DEFAULT_POLL_MS): OverseerHealth | null {
  const tokens = useContext(TokensContext);
  const [health, setHealth] = useState<OverseerHealth | null>(null);

  const load = useCallback(() => fetchHealth(tokens!), [tokens]);
  const onResult = useCallback((h: OverseerHealth) => setHealth(h), []);
  const onError = useCallback(() => undefined, []);

  usePolledFetch(tokens !== null, load, onResult, onError, pollMs);

  return health;
}
