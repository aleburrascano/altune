import { ContractError } from '@shared/errors';
import { NO_PLAYLIST_ID, parsePlaylistId, parseTrackId } from '@shared/api-client/ids';
import {
  asArray,
  asBoolean,
  asNumber,
  asRecord,
  asString,
  nullableNumber,
  nullableString,
} from '@shared/api-client/parse';
import type {
  QueueSourceWire,
  QueueStateCurrentTrack,
  QueueStateResponse,
} from '@shared/api-client/playback';
import type { AcquisitionStatus } from '@shared/api-client/types';
import type { QueueSource, RepeatMode } from '@shared/playback/types';

const SOURCE_KINDS = ['library', 'playlist', 'search'] as const;

export function toWireSource(source: QueueSource | null): QueueSourceWire | null {
  if (!source) return null;
  if (source.kind === 'playlist') {
    return { kind: 'playlist', playlist_id: source.playlistId, name: source.name };
  }
  if (source.kind === 'search') return { kind: 'search', query: source.query };
  return { kind: 'library' };
}

// An unrecognized kind is not a library queue: it is logged and dropped to no source
// rather than being misreported as the library.
export function fromWireSource(source: QueueSourceWire | null | undefined): QueueSource | null {
  if (!source) return null;
  if (source.kind === 'playlist') {
    const parsed = parsePlaylistId(source.playlist_id ?? '');
    return {
      kind: 'playlist',
      playlistId: parsed.ok ? parsed.id : NO_PLAYLIST_ID,
      name: source.name ?? '',
    };
  }
  if (source.kind === 'search') return { kind: 'search', query: source.query ?? '' };
  if (source.kind === 'library') return { kind: 'library' };
  console.warn('[playback] ignored a queue source with an unrecognized kind');
  return null;
}

export function asRepeatMode(value: unknown): RepeatMode | null {
  return value === 'off' || value === 'all' || value === 'one' ? value : null;
}

// An unrecognized status is carried as 'failed' (not playable), never thrown, so one row
// from a newer app version costs only that track, not the whole saved queue.
export function asAcquisitionStatus(value: unknown, at: string): AcquisitionStatus {
  const status = asString(value, at);
  return status === 'pending' || status === 'ready' ? status : 'failed';
}

export type QueueStateParseResult =
  | { ok: true; state: QueueStateResponse }
  | { ok: false; error: ContractError };

function asIndex(value: unknown, at: string): number {
  const n = asNumber(value, at);
  if (!Number.isInteger(n) || n < 0) throw new ContractError(at, 'expected a non-negative integer');
  return n;
}

function asStringArray(value: unknown, at: string): string[] {
  return asArray(value, at).map((item, i) => asString(item, `${at}[${i}]`));
}

function optionalString(value: unknown, at: string): string | undefined {
  return value === undefined ? undefined : asString(value, at);
}

function asSourceKind(value: unknown): QueueSourceWire['kind'] | null {
  return SOURCE_KINDS.find((kind) => kind === value) ?? null;
}

// The saved row is written by whichever app version saved it, so a kind a newer version
// added must cost the reader only its source, not the queue and position beside it.
function parseSource(value: unknown, at: string): QueueSourceWire | null {
  if (value == null) return null;
  const r = asRecord(value, at);
  const kind = asSourceKind(r.kind);
  if (!kind) {
    console.warn(`[playback] dropped ${at}: an unrecognized kind`);
    return null;
  }
  const playlistId = optionalString(r.playlist_id, `${at}.playlist_id`);
  const name = optionalString(r.name, `${at}.name`);
  const query = optionalString(r.query, `${at}.query`);
  return {
    kind,
    ...(playlistId !== undefined ? { playlist_id: playlistId } : {}),
    ...(name !== undefined ? { name } : {}),
    ...(query !== undefined ? { query } : {}),
  };
}

// The current track's id is branded (and so shape-checked) when the queue is rebuilt, so an
// off-shape id is refused here, where it fails the parse instead of throwing mid-restore.
function safeId(value: unknown, at: string): string {
  const id = asString(value, at);
  if (!parseTrackId(id).ok) throw new ContractError(at, 'not a valid id shape');
  return id;
}

function parseCurrentTrack(value: unknown, at: string): QueueStateCurrentTrack {
  const r = asRecord(value, at);
  return {
    id: safeId(r.id, `${at}.id`),
    title: asString(r.title, `${at}.title`),
    artist: asString(r.artist, `${at}.artist`),
    artwork_url: nullableString(r.artwork_url, `${at}.artwork_url`),
    duration_seconds: nullableNumber(r.duration_seconds, `${at}.duration_seconds`),
    acquisition_status: asAcquisitionStatus(r.acquisition_status, `${at}.acquisition_status`),
  };
}

function buildQueueState(value: unknown, at: string): QueueStateResponse {
  const r = asRecord(value, at);
  return {
    track_ids: asStringArray(r.track_ids, `${at}.track_ids`),
    current_index: asIndex(r.current_index, `${at}.current_index`),
    position_ms: asIndex(r.position_ms, `${at}.position_ms`),
    shuffled: asBoolean(r.shuffled, `${at}.shuffled`),
    repeat_mode: asString(r.repeat_mode, `${at}.repeat_mode`),
    source: parseSource(r.source, `${at}.source`),
    // Rows saved before natural order existed carry no natural_order.
    natural_order:
      r.natural_order == null ? [] : asStringArray(r.natural_order, `${at}.natural_order`),
    ...(r.current_track != null
      ? { current_track: parseCurrentTrack(r.current_track, `${at}.current_track`) }
      : {}),
  };
}

// The one boundary between the untrusted queue-state body and the restore path: every
// downstream rebuild step reads the returned state, never the raw response.
export function parseQueueState(value: unknown): QueueStateParseResult {
  try {
    return { ok: true, state: buildQueueState(value, 'QueueStateResponse') };
  } catch (error) {
    if (error instanceof ContractError) return { ok: false, error };
    throw error;
  }
}
