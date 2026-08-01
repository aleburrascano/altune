import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import {
  addTracksToPlaylist,
  createPlaylist,
  deletePlaylist,
  removeTracksFromPlaylist,
  renamePlaylist,
} from '@shared/api-client/playlists';
import type { PlaylistResponse } from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';

type AddTracksVariables = { playlistId: string; trackIds: string[] };
type CreateWithTracksVariables = { name: string; trackIds: string[] };

function alreadyThereMessage(skipped: number, playlistName: string | undefined): string {
  const where = playlistName != null ? `already in ${playlistName}` : 'already in the playlist';
  return skipped === 1 ? `One track was ${where}.` : `${skipped} tracks were ${where}.`;
}

export function useCreatePlaylist() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => createPlaylist({ name }),
    onError: () => {
      Alert.alert('Error', 'Could not create the playlist. Please try again.');
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
      Alert.alert('Error', 'Could not create the playlist. Please try again.');
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
  });
}

export function useAddTracksToPlaylist() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ playlistId, trackIds }: AddTracksVariables) =>
      addTracksToPlaylist(playlistId, { track_ids: trackIds }),
    onMutate: async ({ playlistId, trackIds }) => {
      await queryClient.cancelQueries({ queryKey: playlistKeys.list });
      const previous = queryClient.getQueryData<{ items: PlaylistResponse[] }>(playlistKeys.list);
      if (previous) {
        queryClient.setQueryData<{ items: PlaylistResponse[] }>(playlistKeys.list, {
          ...previous,
          items: previous.items.map((p) =>
            p.id === playlistId ? { ...p, track_count: p.track_count + trackIds.length } : p,
          ),
        });
      }
      return { previous };
    },
    onSuccess: (result, { playlistId, trackIds }) => {
      if (result.added < trackIds.length) {
        const name = queryClient
          .getQueryData<{ items: PlaylistResponse[] }>(playlistKeys.list)
          ?.items.find((p) => p.id === playlistId)?.name;
        Alert.alert('Note', alreadyThereMessage(trackIds.length - result.added, name));
      }
    },
    onError: (_err, { trackIds }, context) => {
      if (context?.previous) {
        queryClient.setQueryData(playlistKeys.list, context.previous);
      }
      Alert.alert(
        'Add failed',
        trackIds.length === 1
          ? 'Could not add the track to the playlist. Please try again.'
          : 'Could not add the tracks to the playlist. Please try again.',
      );
    },
    onSettled: (_data, _error, { playlistId }) =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
        queryClient.invalidateQueries({ queryKey: playlistKeys.detail(playlistId) }),
      ]),
  });
}

export function useRenamePlaylist(playlistId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => renamePlaylist(playlistId, name),
    onMutate: async (name) => {
      await queryClient.cancelQueries({ queryKey: playlistKeys.detail(playlistId) });
      const previous = queryClient.getQueryData(playlistKeys.detail(playlistId));
      if (previous) {
        queryClient.setQueryData(playlistKeys.detail(playlistId), { ...previous, name });
      }
      return { previous };
    },
    onError: (_err, _name, context) => {
      if (context?.previous) {
        queryClient.setQueryData(playlistKeys.detail(playlistId), context.previous);
      }
      Alert.alert('Rename failed', 'Could not rename the playlist. Please try again.');
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: playlistKeys.detail(playlistId) }),
        queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
      ]),
  });
}

export function useDeletePlaylist(playlistId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => deletePlaylist(playlistId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: playlistKeys.list });
      void queryClient.invalidateQueries({ queryKey: playlistKeys.detail(playlistId) });
    },
    onError: () => {
      Alert.alert('Delete failed', 'Could not delete the playlist. Please try again.');
    },
  });
}

export function useRemoveTracksFromPlaylist(playlistId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackIds: string[]) =>
      removeTracksFromPlaylist(playlistId, { track_ids: trackIds }),
    onMutate: async (trackIds) => {
      await queryClient.cancelQueries({ queryKey: playlistKeys.detail(playlistId) });
      const previous = queryClient.getQueryData<{ tracks: { id: string }[] }>(
        playlistKeys.detail(playlistId),
      );
      if (previous) {
        const removing = new Set(trackIds);
        queryClient.setQueryData(playlistKeys.detail(playlistId), {
          ...previous,
          tracks: previous.tracks.filter((t) => !removing.has(t.id)),
        });
      }
      return { previous };
    },
    onError: (_err, trackIds, context) => {
      if (context?.previous) {
        queryClient.setQueryData(playlistKeys.detail(playlistId), context.previous);
      }
      Alert.alert(
        'Remove failed',
        trackIds.length === 1
          ? 'Could not remove the track. Please try again.'
          : 'Could not remove the tracks. Please try again.',
      );
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: playlistKeys.detail(playlistId) }),
        queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
      ]),
  });
}
