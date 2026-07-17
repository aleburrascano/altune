/**
 * isNetworkError — single classification for thrown transport failures
 * (fetch / connectivity / SDK-internal). The heuristic is knowingly
 * approximate; the auth hooks share it so refining it (AbortError, RN's
 * "Network request failed" TypeError, offline detection) is one edit, not
 * four. Network gets distinct, actionable copy (see errorCopy.ts); every
 * other failure stays generic per the anti-enumeration ACs.
 */
export function isNetworkError(err: unknown): boolean {
  return err instanceof Error && /network|fetch|timeout|connection/i.test(err.message);
}
