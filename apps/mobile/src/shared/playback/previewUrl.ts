// The native player fetches whatever scheme it is handed — file://, content:// and friends
// read the device, not the network, and plain http is a target any on-path attacker can
// rewrite — so a third-party provider's preview_url is constrained to https. The one owner of
// that rule: the discover hook drops the source, `toNativeTrack` refuses it at the sink (#1721).
const HTTPS_URL = /^https:\/\/[^\s\u0000-\u001f\u007f]+$/i;

export function isPlayablePreviewUrl(value: unknown): value is string {
  return typeof value === 'string' && HTTPS_URL.test(value);
}

export function getPreviewUrl(extras: Record<string, unknown>): string | null {
  const value = extras['preview_url'];
  return isPlayablePreviewUrl(value) ? value : null;
}
