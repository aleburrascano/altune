/**
 * Mirrored types for the FastAPI catalog endpoints.
 *
 * Hand-maintained for v1; the future OpenAPI codegen spec will replace this
 * with generated types. The mobile and backend shapes must stay in sync —
 * the spec's Risk #2 calls this out, and the plan-reviewer's grep is the
 * mitigation.
 */

export type TrackResponse = {
  id: string; // UUID string from the wire
  title: string;
  artist: string;
  album: string | null;
  duration_seconds: number | null;
  added_at: string; // ISO-8601
};

export type ListTracksResponse = {
  items: TrackResponse[];
  total: number;
  limit: number;
  offset: number;
  has_more: boolean;
};
