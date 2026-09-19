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

// Queue-edit ops (append, insert-next, upcoming reorder) edit the queue the current load
// built instead of claiming a token of their own, and they resolve signed URLs before
// taking the native lock. Reading the token they were computed against fences them
// against everything that replaces that queue: a later full load (#1731) and the
// sign-out reset (#827), which claims one too.
export function currentLoadToken(): number {
  return loadToken;
}

// Sign-out (#827) supersedes the loaded queue like any other load, so the previous user's
// tracks cannot reach the native queue from an op that was already in flight at the reset.
export function claimSessionReset(): void {
  claimLoad();
}
