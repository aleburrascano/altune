export function webOrigin(): string | null {
  return typeof window !== 'undefined' ? window.location?.origin || null : null;
}
