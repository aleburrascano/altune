const REASON_MAP: Record<string, string> = {
  no_match_found: "Couldn't find this track",
  download_failed: 'Download failed',
  ytdlp_error: 'Download error',
};

export function formatFailureReason(reason: string | null): string {
  if (reason == null) return 'Acquisition failed';
  return REASON_MAP[reason] ?? "Couldn't get this track";
}
