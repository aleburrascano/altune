// Load-cancellation guard for the native player. Every full load claims a fresh
// token; an await boundary that observes a newer token knows a later load has
// superseded it and must bail out instead of touching the native queue.
let loadToken = 0;

export function claimLoad(): number {
  return ++loadToken;
}

export function isStale(token: number): boolean {
  return token !== loadToken;
}

// Sign-out guard (#827). Appends and upcoming-reorders do not claim a load token, but
// they resolve signed URLs before taking the native lock; one still in flight at
// sign-out must not re-add the previous user's tracks after the reset. A sign-out
// bumps the epoch (and supersedes any full load); those ops bail if it moved.
let sessionEpoch = 0;

export function claimSessionReset(): void {
  sessionEpoch += 1;
  claimLoad();
}

export function currentSessionEpoch(): number {
  return sessionEpoch;
}
