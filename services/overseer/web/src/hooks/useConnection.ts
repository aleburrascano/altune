import { createContext, useCallback, useContext, useEffect, useReducer } from "react";

export type Conn = "connecting" | "live" | "stalled" | "error";

export type Connection =
  | { conn: "connecting"; lastFrameAt: null }
  | { conn: "live" | "stalled"; lastFrameAt: number }
  | { conn: "error"; lastFrameAt: number | null };

export type ConnectionEvent =
  | { kind: "frame"; at: number }
  | { kind: "failed" }
  | { kind: "silent"; since: number };

export const STALL_AFTER_MS = 6_000;

export const initialConnection: Connection = { conn: "connecting", lastFrameAt: null };

export function nextConnection(current: Connection, event: ConnectionEvent): Connection {
  switch (event.kind) {
    case "frame":
      return { conn: "live", lastFrameAt: event.at };
    case "failed":
      return { conn: "error", lastFrameAt: current.lastFrameAt };
    case "silent":
      return isSilentSinceLastFrame(current, event.since)
        ? { conn: "stalled", lastFrameAt: event.since }
        : current;
  }
}

function isSilentSinceLastFrame(current: Connection, since: number): boolean {
  return current.conn === "live" && current.lastFrameAt === since;
}

export function useConnectionTracker(now: () => number = Date.now) {
  const [connection, dispatch] = useReducer(nextConnection, initialConnection);
  const { conn, lastFrameAt } = connection;

  useEffect(() => {
    if (conn !== "live") return;
    const timer = setTimeout(() => dispatch({ kind: "silent", since: lastFrameAt }), STALL_AFTER_MS);
    return () => clearTimeout(timer);
  }, [conn, lastFrameAt]);

  const markFrame = useCallback(() => dispatch({ kind: "frame", at: now() }), [now]);
  const markFailed = useCallback(() => dispatch({ kind: "failed" }), []);

  return { connection, markFrame, markFailed };
}

export const ConnectionContext = createContext<Connection>(initialConnection);

export function useConnection(): Connection {
  return useContext(ConnectionContext);
}
