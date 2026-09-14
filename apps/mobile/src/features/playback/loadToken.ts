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
