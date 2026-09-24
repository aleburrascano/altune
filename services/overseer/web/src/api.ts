import { createContext } from "react";
import { apiURL } from "./config";
import type { Range, SeriesResponse, Snapshot } from "./types";

// TokenProvider yields the current bearer token and can force a refresh when the
// server reports 401 (the access token expired). Both return null when there is no
// usable session, which the caller surfaces as "signed out".
export interface TokenProvider {
  get(): Promise<string | null>;
  refresh(): Promise<string | null>;
}

// authedFetch attaches the bearer token and, on a 401, transparently refreshes the
// token once and retries — so an expired access token never blanks the UI. It
// never sends credentials (cookies): auth is bearer only.
export async function authedFetch(
  path: string,
  tokens: TokenProvider,
  init: RequestInit = {},
): Promise<Response> {
  const token = await tokens.get();
  if (!token) throw new UnauthorizedError();

  const res = await doFetch(path, token, init);
  if (res.status !== 401) return res;

  // Token likely expired mid-session: refresh once and retry.
  const fresh = await tokens.refresh();
  if (!fresh) throw new UnauthorizedError();
  return doFetch(path, fresh, init);
}

function doFetch(path: string, token: string, init: RequestInit): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${token}`);
  return fetch(apiURL(path), { ...init, headers, credentials: "omit" });
}

// UnauthorizedError signals the session is gone (no token, or refresh failed), so
// the UI returns to the login screen rather than spinning.
export class UnauthorizedError extends Error {
  constructor() {
    super("unauthorized");
    this.name = "UnauthorizedError";
  }
}

// ForbiddenError signals a valid but non-owner token: authenticated, not allowed.
export class ForbiddenError extends Error {
  constructor() {
    super("forbidden");
    this.name = "ForbiddenError";
  }
}

interface BucketsResponse {
  buckets: Snapshot[];
}

// fetchBuckets returns every bucket's current snapshot, ID-sorted by the server.
export async function fetchBuckets(tokens: TokenProvider): Promise<Snapshot[]> {
  const res = await authedFetch("api/buckets", tokens);
  if (res.status === 403) throw new ForbiddenError();
  if (!res.ok) throw new Error(`api/buckets: HTTP ${res.status}`);
  const body = (await res.json()) as BucketsResponse;
  return body.buckets ?? [];
}

export async function fetchSeries(
  tokens: TokenProvider,
  id: string,
  range: Range,
): Promise<SeriesResponse> {
  const path = `api/buckets/${encodeURIComponent(id)}/series?range=${range}`;
  const res = await authedFetch(path, tokens);
  if (res.status === 403) throw new ForbiddenError();
  if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
  const body = (await res.json()) as SeriesResponse;
  return { ...body, series: body.series ?? {} };
}

export const TokensContext = createContext<TokenProvider | null>(null);

export interface StreamHandlers {
  onSnapshot: (snap: Snapshot) => void;
  onError?: (err: unknown) => void;
}

// openStream reads the SSE live channel via a fetch ReadableStream (not native
// EventSource, which cannot send the bearer header). It reconnects on a 401 with a
// refreshed token and on a dropped connection with a bounded backoff, until the
// AbortSignal fires. A refreshed token that keeps getting 401 backs off
// exponentially rather than tight-looping. Per-read buffering is bounded to the
// current frame.
export async function openStream(
  tokens: TokenProvider,
  handlers: StreamHandlers,
  signal: AbortSignal,
): Promise<void> {
  let backoff = 500;
  let authRetries = 0;
  while (!signal.aborted) {
    try {
      const token = await tokens.get();
      if (!token) throw new UnauthorizedError();
      const res = await fetch(apiURL("api/stream"), {
        headers: { Authorization: `Bearer ${token}`, Accept: "text/event-stream" },
        credentials: "omit",
        signal,
      });
      if (res.status === 401) {
        const fresh = await tokens.refresh();
        if (!fresh) throw new UnauthorizedError();
        // First 401 is the normal expiry case: reconnect immediately with the fresh
        // token. If a *refreshed* token keeps 401ing (server rejecting a token it
        // just handed out), back off exponentially so the reconnect+refresh path
        // cannot become a tight loop hammering the server.
        authRetries++;
        if (authRetries > 1) {
          await sleep(Math.min(500 * 2 ** (authRetries - 2), 10_000), signal);
        }
        continue;
      }
      if (res.status === 403) throw new ForbiddenError();
      if (!res.ok || !res.body) throw new Error(`api/stream: HTTP ${res.status}`);

      backoff = 500; // a real connection resets the backoff
      authRetries = 0; // ...and clears the 401-retry backoff
      await readFrames(res.body, handlers.onSnapshot, signal);
    } catch (err) {
      if (signal.aborted) return;
      if (err instanceof UnauthorizedError || err instanceof ForbiddenError) {
        handlers.onError?.(err);
        return;
      }
      handlers.onError?.(err);
    }
    if (signal.aborted) return;
    await sleep(backoff, signal);
    backoff = Math.min(backoff * 2, 10_000);
  }
}

// readFrames decodes SSE `data:` frames off the stream and calls onSnapshot for
// each. It holds only the current partial frame in memory (bounded), splitting on
// the blank-line frame delimiter.
async function readFrames(
  body: ReadableStream<Uint8Array>,
  onSnapshot: (snap: Snapshot) => void,
  signal: AbortSignal,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    while (!signal.aborted) {
      const { value, done } = await reader.read();
      if (done) return;
      buffer += decoder.decode(value, { stream: true });
      let sep: number;
      while ((sep = buffer.indexOf("\n\n")) !== -1) {
        const frame = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        emitFrame(frame, onSnapshot);
      }
    }
  } finally {
    reader.releaseLock();
  }
}

function emitFrame(frame: string, onSnapshot: (snap: Snapshot) => void): void {
  const data = frame
    .split("\n")
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trim())
    .join("");
  if (!data) return;
  try {
    onSnapshot(JSON.parse(data) as Snapshot);
  } catch {
    // A malformed frame is skipped, never fatal to the stream.
  }
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const id = setTimeout(resolve, ms);
    signal.addEventListener("abort", () => {
      clearTimeout(id);
      resolve();
    }, { once: true });
  });
}
