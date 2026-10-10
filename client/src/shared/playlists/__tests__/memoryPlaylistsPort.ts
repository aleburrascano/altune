import type { PlaylistId, TrackId } from '@shared/apiClient/ids';
import { asPlaylistId } from '@shared/apiClient/ids';
import type { PlaylistDetailResponse, PlaylistResponse } from '@shared/apiClient/playlistResponse';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import { ApiError } from '@shared/failure/errors';

import type { PlaylistsPort } from '@shared/playlists/ports/playlistsPort';

const NOT_FOUND_STATUS = 404;

interface StoredPlaylist {
  summary: PlaylistResponse;
  trackIds: TrackId[];
}

function abortError(): Error {
  return Object.assign(new Error('The operation was aborted'), { name: 'AbortError' });
}

function rejectIfAborted(signal: AbortSignal | undefined): void {
  if (signal?.aborted === true) throw abortError();
}

async function resolveOnLaterTick<T>(
  signal: AbortSignal | undefined,
  produce: () => T,
): Promise<T> {
  rejectIfAborted(signal);
  await Promise.resolve();
  rejectIfAborted(signal);
  return produce();
}

function storedTrack(id: TrackId, addedAt: string): TrackResponse {
  return {
    id,
    title: id,
    artist: 'Artist',
    album: null,
    duration_seconds: null,
    added_at: addedAt,
    artwork_url: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    acquisition_status: 'ready',
    failure_reason: null,
  };
}

function detailOf({ summary, trackIds }: StoredPlaylist): PlaylistDetailResponse {
  const tracks = trackIds.map((id) => storedTrack(id, summary.updated_at));
  return { ...summary, total_duration_seconds: 0, tracks };
}

function notFound(id: PlaylistId): ApiError {
  return new ApiError(NOT_FOUND_STATUS, `playlist ${id} not found`);
}

export function createMemoryPlaylistsPort(): PlaylistsPort {
  const stored = new Map<PlaylistId, StoredPlaylist>();
  let nextId = 1;

  function find(id: PlaylistId): StoredPlaylist {
    const found = stored.get(id);
    if (found === undefined) throw notFound(id);
    return found;
  }

  function sync(entry: StoredPlaylist): PlaylistResponse {
    entry.summary = {
      ...entry.summary,
      track_count: entry.trackIds.length,
      updated_at: new Date().toISOString(),
    };
    return entry.summary;
  }

  return {
    getPlaylists: ({ limit, offset = 0 } = {}, signal) =>
      resolveOnLaterTick(signal, () => {
        const all = [...stored.values()].map((entry) => entry.summary);
        const end = limit === undefined ? all.length : offset + limit;
        return { items: all.slice(offset, end), total: all.length };
      }),
    getPlaylist: async (id) => detailOf(find(id)),
    createPlaylist: async ({ name }) => {
      const now = new Date().toISOString();
      const summary: PlaylistResponse = {
        id: asPlaylistId(`p${nextId++}`),
        name,
        track_count: 0,
        preview_artwork_urls: [],
        created_at: now,
        updated_at: now,
      };
      stored.set(summary.id, { summary, trackIds: [] });
      return summary;
    },
    renamePlaylist: async (id, name) => {
      const entry = find(id);
      entry.summary = { ...entry.summary, name };
      return sync(entry);
    },
    deletePlaylist: async (id) => {
      find(id);
      stored.delete(id);
    },
    addTracksToPlaylist: async (id, { track_ids }) => {
      const entry = find(id);
      const fresh = [...new Set(track_ids)].filter((trackId) => !entry.trackIds.includes(trackId));
      entry.trackIds.push(...fresh);
      sync(entry);
      return { added: fresh.length, skipped: track_ids.length - fresh.length };
    },
    removeTracksFromPlaylist: async (id, { track_ids }) => {
      const entry = find(id);
      const before = entry.trackIds.length;
      entry.trackIds = entry.trackIds.filter((trackId) => !track_ids.includes(trackId));
      sync(entry);
      return { removed: before - entry.trackIds.length };
    },
  };
}
