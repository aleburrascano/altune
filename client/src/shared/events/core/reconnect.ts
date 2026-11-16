const BASE_RECONNECT_MS = 1_000;
const MAX_RECONNECT_MS = 30_000;
const MAX_RETRY_AFTER_MS = 5 * 60_000;

export function reconnectDelayMs(attempt: number, minimumDelayMs: number, jitter: number): number {
  const base = Math.min(BASE_RECONNECT_MS * 2 ** attempt, MAX_RECONNECT_MS);
  return Math.max(base + jitter * base, minimumDelayMs);
}

export function isRefusal(status: number): boolean {
  return status >= 300;
}

export function retryAfterMs(header: string | null, now: number): number {
  if (header === null) return 0;
  const value = header.trim();
  const wait = /^\d+$/.test(value) ? Number(value) * 1_000 : Date.parse(value) - now;
  if (Number.isNaN(wait)) return 0;
  return Math.min(Math.max(wait, 0), MAX_RETRY_AFTER_MS);
}
