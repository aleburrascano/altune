export function normalizeForCompare(s: string | null): string {
  return (s ?? '').toLowerCase().trim();
}
