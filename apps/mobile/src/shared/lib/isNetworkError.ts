/**
 * isNetworkError — single classification for thrown transport failures
 * (fetch / connectivity / SDK-internal). The heuristic is knowingly
 * approximate; every caller shares it so refining it (AbortError, RN's
 * "Network request failed" TypeError, offline detection) is one edit.
 *
 * Promoted out of `features/auth/lib/` when Library became a second consumer:
 * a failed library fetch must say "you're offline" rather than blame the
 * server, and the app has no connectivity API (NetInfo is not a dependency —
 * it is a native module, so adding it needs a new dev build). Classifying the
 * error we already caught is the honest signal available without one.
 *
 * Auth additionally maps 'network' to distinct copy (see auth/lib/errorCopy.ts);
 * every other failure stays generic per the anti-enumeration ACs.
 */
export function isNetworkError(err: unknown): boolean {
  return err instanceof Error && /network|fetch|timeout|connection/i.test(err.message);
}
