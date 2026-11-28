export type BackfillFeaturedResult = {
  scanned: number;
  updated: number;
  truncated: boolean;
  nextOffset: number;
};
