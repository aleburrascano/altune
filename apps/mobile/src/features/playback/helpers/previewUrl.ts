export function getPreviewUrl(extras: Record<string, unknown>): string | null {
  const value = extras['preview_url'];
  return typeof value === 'string' && value.length > 0 ? value : null;
}
