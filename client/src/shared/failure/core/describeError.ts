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

function copyForCode(code: ApiErrorCode): ErrorCopy | undefined {
  switch (code) {
    case 'catalog.track_already_in_playlist':
      return { title: 'Already in this playlist', body: 'That track is already in this playlist.' };
    case 'catalog.playlist_full':
      return {
        title: 'Playlist is full',
        body: 'Remove a track from this playlist to add another.',
      };
    case 'catalog.too_many_playlists':
      return { title: 'Too many playlists', body: 'Delete a playlist to create another.' };
    case 'catalog.library_full':
      return { title: 'Library is full', body: 'Remove a track from your library to add another.' };
    case 'discovery.favorites_full':
      return { title: 'Favorites are full', body: 'Remove a favorite to add another.' };
    default:
      return undefined;
  }
}

export function describeError(err: unknown, action?: string): ErrorCopy {
  const specific =
    err instanceof ApiError && err.code !== undefined ? copyForCode(err.code) : undefined;
  const copy = specific ?? COPY_BY_KIND[failureKindOf(err)];
  if (action === undefined || specific !== undefined) return copy;
  return { title: `Couldn't ${action}`, body: copy.body };
}
