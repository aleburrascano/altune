import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import {
  addTracksToPlaylist,
  createPlaylist,
  deletePlaylist,
  removeTracksFromPlaylist,
  renamePlaylist,
} from '@shared/api-client/playlists';
import type { PlaylistId, TrackId } from '@shared/api-client/ids';
import type { PlaylistResponse } from '@shared/api-client/types';
import { RETRY_TAIL } from '@shared/lib/describeError';
import { countLabel } from '@shared/lib/format';
import { playlistKeys } from '@shared/lib/query-keys';
import { useOptimisticMutation } from '@shared/query/useOptimisticMutation';

type AddTracksVariables = { playlistId: PlaylistId; trackIds: TrackId[] };
type CreateWithTracksVariables = { name: string; trackIds: TrackId[] };
type PlaylistList = { items: PlaylistResponse[] };
type PlaylistDetail = { name: string; tracks: { id: TrackId }[] };

function alreadyThereMessage(skipped: number, playlistName: string | undefined): string {
  const where = playlistName != null ? `already in ${playlistName}` : 'already in the playlist';
  return skipped === 1 ? `One track was ${where}.` : `${skipped} tracks were ${where}.`;
}

export function useCreatePlaylist() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => createPlaylist({ name }),
    onError: () => {
      Alert.alert('Error', `Could not create the playlist. ${RETRY_TAIL}`);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
  });
}

export function useCreatePlaylistWithTracks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ name, trackIds }: CreateWithTracksVariables) => {
      const playlist = await createPlaylist({ name });
      try {
        const { added } = await addTracksToPlaylist(playlist.id, { track_ids: trackIds });
        return { playlist, added, addFailed: false };
      } catch {
        return { playlist, added: 0, addFailed: true };
      }
    },
    onSuccess: ({ addFailed, added, playlist }, { trackIds }) => {
      if (addFailed) {
        Alert.alert(
          'Note',
          trackIds.length === 1
            ? 'Playlist created, but the track could not be added. Try adding it manually.'
            : 'Playlist created, but the tracks could not be added. Try adding them manually.',
        );
        return;
      }
      if (added < trackIds.length) {
        Alert.alert('Note', alreadyThereMessage(trackIds.length - added, playlist.name));
      }
    },
    onError: () => {
      Alert.alert('Error', `Could not create the playlist. ${RETRY_TAIL}`);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
  });
}

export function useAddTracksToPlaylist() {
  const queryClient = useQueryClient();
  return useOptimisticMutation({
    queryKey: playlistKeys.list,
    mutationFn: ({ playlistId, trackIds }: AddTracksVariables) =>
      addTracksToPlaylist(playlistId, { track_ids: trackIds }),
    applyOptimistic: (previous: PlaylistList, { playlistId, trackIds }) => ({
      ...previous,
      items: previous.items.map((p) =>
        p.id === playlistId ? { ...p, track_count: p.track_count + trackIds.length } : p,
      ),
    }),
    onSuccess: (result, { playlistId, trackIds }) => {
      if (result.added < trackIds.length) {
        const name = queryClient
          .getQueryData<PlaylistList>(playlistKeys.list)
          ?.items.find((p) => p.id === playlistId)?.name;
        Alert.alert('Note', alreadyThereMessage(trackIds.length - result.added, name));
      }
    },
    alertOnError: ({ trackIds }) => ({
      title: 'Add failed',
      message: `Could not add the ${countLabel(trackIds.length, 'track')} to the playlist. ${RETRY_TAIL}`,
    }),
    invalidate: ({ playlistId }) => [playlistKeys.list, playlistKeys.detail(playlistId)],
  });
}

export function useRenamePlaylist(playlistId: PlaylistId) {
  return useOptimisticMutation({
    queryKey: playlistKeys.detail(playlistId),
    mutationFn: (name: string) => renamePlaylist(playlistId, name),
    applyOptimistic: (previous: PlaylistDetail, name) => ({ ...previous, name }),
    alertOnError: () => ({
      title: 'Rename failed',
      message: `Could not rename the playlist. ${RETRY_TAIL}`,
    }),
    invalidate: () => [playlistKeys.detail(playlistId), playlistKeys.list],
  });
}

export function useDeletePlaylist(playlistId: PlaylistId) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => deletePlaylist(playlistId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: playlistKeys.list });
      void queryClient.invalidateQueries({ queryKey: playlistKeys.detail(playlistId) });
    },
    onError: () => {
      Alert.alert('Delete failed', `Could not delete the playlist. ${RETRY_TAIL}`);
    },
  });
}

export function useRemoveTracksFromPlaylist(playlistId: PlaylistId) {
  return useOptimisticMutation({
    queryKey: playlistKeys.detail(playlistId),
    mutationFn: (trackIds: TrackId[]) =>
      removeTracksFromPlaylist(playlistId, { track_ids: trackIds }),
    applyOptimistic: (previous: PlaylistDetail, trackIds) => {
      const removing = new Set(trackIds);
      return { ...previous, tracks: previous.tracks.filter((t) => !removing.has(t.id)) };
    },
    alertOnError: (trackIds) => ({
      title: 'Remove failed',
      message: `Could not remove the ${countLabel(trackIds.length, 'track')}. ${RETRY_TAIL}`,
    }),
    invalidate: () => [playlistKeys.detail(playlistId), playlistKeys.list],
  });
}
