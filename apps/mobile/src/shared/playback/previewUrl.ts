const HTTPS_URL = /^https:\/\/[^\s\u0000-\u001f\u007f]+$/i;

export function isPlayablePreviewUrl(value: unknown): value is string {
  return typeof value === 'string' && HTTPS_URL.test(value);
}

export function getPreviewUrl(extras: Record<string, unknown>): string | null {
  const value = extras['preview_url'];
  return isPlayablePreviewUrl(value) ? value : null;
}
