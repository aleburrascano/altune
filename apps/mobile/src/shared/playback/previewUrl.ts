// The native player accepts whatever scheme it is handed — file://, content://
// and friends read the device, not the network — so a third-party provider's
// preview_url is constrained to http(s) here, before it reaches that sink.
const HTTP_URL = /^https?:\/\/[^\s\u0000-\u001f\u007f]+$/i;

function isHttpUrl(value: unknown): value is string {
  return typeof value === 'string' && HTTP_URL.test(value);
}

export function getPreviewUrl(extras: Record<string, unknown>): string | null {
  const value = extras['preview_url'];
  return isHttpUrl(value) ? value : null;
}
