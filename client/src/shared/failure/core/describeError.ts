import { ApiError } from '@shared/failure/core/errors';
import { failureKindOf, type FailureKind } from '@shared/failure/core/failureKind';
import type { ApiErrorCode } from '@shared/failure/core/apiErrorCodes';

export interface ErrorCopy {
  readonly title: string;
  readonly body: string;
}

export const RETRY_TAIL = 'Please try again.';

const GENERIC_TITLE = 'Something went wrong';

const COPY_BY_KIND: Readonly<Record<FailureKind, ErrorCopy>> = {
  contract: {
    title: 'Update required',
    body: 'This version of Altune is out of date. Update the app to continue.',
  },
  offline: { title: 'No connection', body: 'Check your connection and try again.' },
  timeout: { title: GENERIC_TITLE, body: `The request took too long. ${RETRY_TAIL}` },
  server: { title: GENERIC_TITLE, body: `Something went wrong on our end. ${RETRY_TAIL}` },
  'rate-limited': {
    title: 'Too many requests',
    body: 'You are doing that too often. Wait a moment, then try again.',
  },
  unauthorized: {
    title: 'Sign in again',
    body: 'Your session has expired. Sign in again, then retry.',
  },
  'not-found': { title: GENERIC_TITLE, body: 'That is no longer available.' },
  conflict: { title: GENERIC_TITLE, body: 'That conflicts with what is already saved.' },
  'limit-reached': { title: GENERIC_TITLE, body: 'You have reached a limit for that.' },
  rejected: { title: GENERIC_TITLE, body: 'We could not complete that request.' },
  unknown: { title: GENERIC_TITLE, body: `Something went wrong. ${RETRY_TAIL}` },
};

const COPY_BY_CODE: Readonly<Partial<Record<ApiErrorCode, ErrorCopy>>> = {
  'catalog.track_already_in_playlist': {
    title: 'Already in this playlist',
    body: 'That track is already in this playlist.',
  },
  'catalog.playlist_full': {
    title: 'Playlist is full',
    body: 'Remove a track from this playlist to add another.',
  },
  'catalog.too_many_playlists': {
    title: 'Too many playlists',
    body: 'Delete a playlist to create another.',
  },
  'catalog.library_full': {
    title: 'Library is full',
    body: 'Remove a track from your library to add another.',
  },
  'discovery.favorites_full': {
    title: 'Favorites are full',
    body: 'Remove a favorite to add another.',
  },
  'catalog.batch_too_large': {
    title: 'Too many tracks at once',
    body: 'Pick fewer tracks and try again.',
  },
  'playback.queue_too_long': {
    title: 'Queue is too long',
    body: 'Remove some tracks from the queue, then try again.',
  },
  'acquisition.retry_not_failed': {
    title: 'Nothing to retry',
    body: 'This track has not failed, so there is nothing to retry.',
  },
  'acquisition.reacquire_not_ready': {
    title: 'Not ready yet',
    body: "This track isn't ready to re-download yet. Try again in a minute.",
  },
  'discovery.provider_rate_limited': {
    title: 'Music source is busy',
    body: 'The music source is busy. Try again in a minute.',
  },
};

function copyForCode(code: ApiErrorCode): ErrorCopy | undefined {
  return COPY_BY_CODE[code];
}

export function describeError(err: unknown, action?: string): ErrorCopy {
  const specific =
    err instanceof ApiError && err.code !== undefined ? copyForCode(err.code) : undefined;
  const copy = specific ?? COPY_BY_KIND[failureKindOf(err)];
  if (action === undefined || specific !== undefined) return copy;
  return { title: `Couldn't ${action}`, body: copy.body };
}
