import { createContext } from "react";
import { apiURL } from "./config";
import type { OverseerHealth, Range, SeriesResponse, Snapshot } from "./types";

export interface TokenProvider {
  get(): Promise<string | null>;
  refresh(): Promise<string | null>;
}

export async function authedFetch(
  path: string,
  tokens: TokenProvider,
  init: RequestInit = {},
): Promise<Response> {
  const token = await tokens.get();
  if (!token) throw new UnauthorizedError();

  const res = await doFetch(path, token, init);
  if (res.status !== 401) return res;

  const fresh = await tokens.refresh();
  if (!fresh) throw new UnauthorizedError();
  return doFetch(path, fresh, init);
}

function doFetch(path: string, token: string, init: RequestInit): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${token}`);
  return fetch(apiURL(path), { ...init, headers, credentials: "omit" });
}

export class UnauthorizedError extends Error {
  constructor() {
    super("unauthorized");
    this.name = "UnauthorizedError";
  }
}

export class ForbiddenError extends Error {
  constructor() {
    super("forbidden");
    this.name = "ForbiddenError";
  }
}

interface BucketsResponse {
  buckets: Snapshot[];
}

export async function fetchBuckets(tokens: TokenProvider): Promise<Snapshot[]> {
  const res = await authedFetch("api/buckets", tokens);
  if (res.status === 403) throw new ForbiddenError();
  if (!res.ok) throw new Error(`api/buckets: HTTP ${res.status}`);
  const body = (await res.json()) as BucketsResponse;
  return body.buckets ?? [];
}

export async function fetchHealth(tokens: TokenProvider): Promise<OverseerHealth> {
  const res = await authedFetch("api/health", tokens);
  if (res.status === 403) throw new ForbiddenError();
  if (!res.ok) throw new Error(`api/health: HTTP ${res.status}`);
  return (await res.json()) as OverseerHealth;
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
        authRetries++;
        if (authRetries > 1) {
          await sleep(Math.min(500 * 2 ** (authRetries - 2), 10_000), signal);
        }
        continue;
      }
      if (res.status === 403) throw new ForbiddenError();
      if (!res.ok || !res.body) throw new Error(`api/stream: HTTP ${res.status}`);

      backoff = 500;
      authRetries = 0;
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
    return;
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
