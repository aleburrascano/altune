const DEFAULT_STALE_MS = 30_000;
const MINUTE_STALE_MS = 60_000;
const HALF_HOUR_STALE_MS = 1_800_000;
const DAY_STALE_MS = 86_400_000;

export const STALE = {
  default: DEFAULT_STALE_MS,
  minute: MINUTE_STALE_MS,
  halfHour: HALF_HOUR_STALE_MS,
  day: DAY_STALE_MS,
  now: 0,
  untilEvent: Infinity,
} as const;
