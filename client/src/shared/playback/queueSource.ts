import type { PlaylistId } from '@shared/api-client/ids';

export type RepeatMode = 'off' | 'all' | 'one';

export type QueueSource =
  | { readonly kind: 'playlist'; readonly playlistId: PlaylistId; readonly name: string }
  | { readonly kind: 'library' }
  | { readonly kind: 'search'; readonly query: string };
