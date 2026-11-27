import type { ApiErrorCode } from '@shared/failure/core/apiErrorCodes';
import {
  HTTP_CLIENT_ERROR_STATUS,
  HTTP_CONFLICT,
  HTTP_FORBIDDEN,
  HTTP_GONE,
  HTTP_NOT_FOUND,
  HTTP_REQUEST_TIMEOUT,
  HTTP_SERVER_ERROR_STATUS,
  HTTP_TOO_MANY_REQUESTS,
  HTTP_UNAUTHORIZED,
} from '@shared/failure/core/httpStatus';
import {
  ApiError,
  NetworkError,
  isContractError,
  isNetworkError,
} from '@shared/failure/core/errors';

export type FailureKind =
  | 'offline'
  | 'timeout'
  | 'unauthorized'
  | 'not-found'
  | 'rate-limited'
  | 'conflict'
  | 'limit-reached'
  | 'server'
  | 'rejected'
  | 'contract'
  | 'unknown';

export function kindOfStatus(status: number): FailureKind {
  if (status === HTTP_UNAUTHORIZED || status === HTTP_FORBIDDEN) return 'unauthorized';
  if (status === HTTP_NOT_FOUND || status === HTTP_GONE) return 'not-found';
  if (status === HTTP_REQUEST_TIMEOUT) return 'timeout';
  if (status === HTTP_TOO_MANY_REQUESTS) return 'rate-limited';
  if (status === HTTP_CONFLICT) return 'conflict';
  if (status >= HTTP_SERVER_ERROR_STATUS) return 'server';
  if (status >= HTTP_CLIENT_ERROR_STATUS) return 'rejected';
  return 'unknown';
}

export function kindOfCode(code: ApiErrorCode): FailureKind {
  switch (code) {
    case 'auth.token_invalid':
    case 'auth.token_missing':
    case 'auth.token_malformed':
    case 'auth.token_signature_invalid':
    case 'auth.token_expired':
    case 'auth.token_revoked':
    case 'auth.token_claim_missing_exp':
    case 'auth.token_claim_invalid_iss':
    case 'auth.token_claim_invalid_aud':
    case 'auth.token_claim_invalid_sub':
    case 'auth.token_claim_invalid_iat':
    case 'observe.principal_required':
      return 'unauthorized';
    case 'catalog.track_not_found':
    case 'catalog.playlist_not_found':
    case 'catalog.audio_not_available':
    case 'catalog.audio_orphaned':
    case 'discovery.content_unserved':
      return 'not-found';
    case 'auth.throttled':
    case 'discovery.rate_limited':
    case 'discovery.provider_rate_limited':
    case 'playback.rate_limited':
    case 'catalog.audio_rate_limited':
    case 'catalog.write_rate_limited':
    case 'catalog.backfill_cooling_down':
    case 'acquisition.cooldown_active':
      return 'rate-limited';
    case 'catalog.track_already_in_playlist':
    case 'catalog.backfill_in_progress':
    case 'acquisition.job_in_flight':
    case 'playback.stale_queue_write':
    case 'playback.queue_position_mismatch':
      return 'conflict';
    case 'catalog.playlist_full':
    case 'catalog.too_many_playlists':
    case 'catalog.library_full':
    case 'discovery.favorites_full':
    case 'playback.queue_too_long':
    case 'request.too_large':
    case 'catalog.batch_too_large':
    case 'eventstream.too_many_streams':
    case 'observe.stream_subscriber_limit':
      return 'limit-reached';
    case 'request.timeout':
    case 'discovery.provider_timeout':
    case 'observe.discography_quality_timeout':
      return 'timeout';
    case 'internal':
    case 'auth.unavailable':
    case 'catalog.audio_temporarily_unavailable':
    case 'catalog.temporarily_unavailable':
    case 'discovery.all_providers_failed':
    case 'discovery.provider_circuit_open':
    case 'discovery.provider_error':
    case 'enrichment_unavailable':
    case 'playback.unavailable':
    case 'acquisition.shutting_down':
    case 'acquisition.paused':
    case 'eventstream.streaming_unsupported':
    case 'observe.event_feed_unavailable':
    case 'observe.streaming_unsupported':
    case 'observe.stream_unavailable':
      return 'server';
    case 'request.invalid_id':
    case 'request.invalid_body':
    case 'auth_failure.invalid_body':
    case 'observe.invalid_group_by':
    case 'observe.invalid_window_days':
    case 'catalog.track_ids_required':
    case 'catalog.invalid_track_id':
    case 'catalog.featured_artist_key_required':
    case 'catalog.invalid_deezer_id':
    case 'catalog.validation_error':
    case 'discovery.invalid_favorite':
    case 'discovery.invalid_event':
    case 'discovery.invalid_body':
    case 'discovery.invalid_event_type':
    case 'discovery.invalid_kind':
    case 'discovery.invalid_param':
    case 'discovery.invalid_provider':
    case 'discovery.q_required':
    case 'playback.position_ms_negative':
    case 'playback.string_too_long':
    case 'playback.string_contains_nul':
    case 'playback.current_idx_out_of_range':
    case 'playback.current_track_id_missing':
    case 'playback.unknown_repeat_mode':
    case 'playback.unknown_source_kind':
    case 'playback.unrecognized_source_id':
    case 'playback.malformed_track_id':
    case 'playback.natural_order_mismatch':
    case 'acquisition.retry_not_failed':
    case 'acquisition.reacquire_not_ready':
      return 'rejected';
    default:
      return assertNever(code);
  }
}

function assertNever(code: never): FailureKind {
  throw new Error(`unhandled API error code ${String(code)}`);
}

export function failureKindOf(error: unknown): FailureKind {
  if (isContractError(error)) return 'contract';
  if (error instanceof NetworkError && error.failure === 'timeout') return 'timeout';
  if (isNetworkError(error)) return 'offline';
  if (!(error instanceof ApiError)) return 'unknown';
  const fromCode = error.code === undefined ? 'unknown' : kindOfCode(error.code);
  return fromCode === 'unknown' ? kindOfStatus(error.status) : fromCode;
}
