const DEFAULT_STALE_MS = 30_000;
const MINUTE_STALE_MS = 60_000;

export const STALE = {
  default: DEFAULT_STALE_MS,
  minute: MINUTE_STALE_MS,
  now: 0,
  untilEvent: Infinity,
} as const;
