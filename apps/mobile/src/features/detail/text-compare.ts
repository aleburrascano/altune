// A missing side folds to the empty string, so it matches only another missing
// or blank side — never a real title or artist.
export function normalizeForCompare(s: string | null): string {
  return (s ?? '').toLowerCase().trim();
}
