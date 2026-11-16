import type { TrackResponse } from '@shared/catalog';
import {
  pagedAccessor,
  PATCH_POLICY,
  reflowOffsets,
  type TrackPages,
} from '@shared/events/core/trackCacheShapes';

function mergeTrack(prev: TrackResponse, next: TrackResponse): TrackResponse {
  const merged = { ...prev, ...next };
  if (next.failure_message === undefined) delete merged.failure_message;
  return merged as TrackResponse;
}

function holds(pages: TrackPages, track: TrackResponse): boolean {
  return pages.pages.some((page) => page.items.some((t) => t.id === track.id));
}

function mergeInto(pages: TrackPages, track: TrackResponse): TrackPages | undefined {
  return pagedAccessor.write(
    pages,
    (items) => items.map((t) => (t.id === track.id ? mergeTrack(t, track) : t)),
    PATCH_POLICY,
  );
}

function prepend(pages: TrackPages, track: TrackResponse): TrackPages {
  const [first, ...rest] = pages.pages;
  if (!first) return pages;
  const next = [{ ...first, items: [track, ...first.items], total: first.total + 1 }, ...rest];
  return { ...pages, pages: reflowOffsets(pages.pages, next) };
}

export function upsertIntoPages(
  prev: TrackPages | undefined,
  track: TrackResponse,
): TrackPages | undefined {
  if (!prev) return prev;
  return holds(prev, track) ? mergeInto(prev, track) : prepend(prev, track);
}
