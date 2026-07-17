/**
 * usePlaylistMutations — every playlist write goes through here, so the
 * optimistic-patch → rollback → invalidate choreography over the playlists
 * list / playlist detail caches has one owner (structure audit F1). Before
 * this, the six mutations lived in three files and their policies had
 * diverged (create-playlist failed silently in one path, alerted in another).
 *
 * Policy: every mutation alerts on failure; optimistic patches roll back to
 * the onMutate snapshot; onSettled invalidates so the authoritative server
 * state reconciles (the SSE event patches cover other devices — F15).
 * Screen-local concerns (closing a modal, navigating away) stay in the
 * screens via mutate-level callbacks.
 */
import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import {
  addTrackToPlaylist,
  createPlaylist,
  deletePlaylist,
  removeTrackFromPlaylist,
  renamePlaylist,
} from '@shared/api-client/playlists';
import type { PlaylistResponse } from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';

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

/**
 * Create a playlist, then add the given track to it — the "New Playlist" path
 * inside the add-to-playlist sheet. A failed add after a successful create is
 * reported but not rolled back (the playlist exists; the user adds manually).
 */
export function useCreatePlaylistWithTrack(trackId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (name: string) => {
      const pl = await createPlaylist({ name });
      let addFailed = false;
      try {
        await addTrackToPlaylist(pl.id, { track_id: trackId });
      } catch {
        addFailed = true;
      }
      return { pl, addFailed };
    },
    onSuccess: ({ addFailed }) => {
      if (addFailed) {
        Alert.alert('Note', 'Playlist created, but the track could not be added. Try adding it manually.');
      }
    },
    onError: () => {
      Alert.alert('Error', 'Could not create the playlist. Please try again.');
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
  });
}

/** Add a track to an existing playlist, optimistically bumping its count. */
export function useAddTrackToPlaylist(trackId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (playlistId: string) => addTrackToPlaylist(playlistId, { track_id: trackId }),
    onMutate: async (playlistId) => {
      await queryClient.cancelQueries({ queryKey: playlistKeys.list });
      const previous = queryClient.getQueryData<{ items: PlaylistResponse[] }>(playlistKeys.list);
      if (previous) {
        queryClient.setQueryData<{ items: PlaylistResponse[] }>(playlistKeys.list, {
          ...previous,
          items: previous.items.map((p) =>
            p.id === playlistId ? { ...p, track_count: p.track_count + 1 } : p,
          ),
        });
      }
      return { previous };
    },
    onError: (_err, _playlistId, context) => {
      if (context?.previous) {
        queryClient.setQueryData(playlistKeys.list, context.previous);
      }
      Alert.alert('Add failed', 'Could not add the track to the playlist. Please try again.');
    },
    onSettled: (_data, _error, playlistId) =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
        queryClient.invalidateQueries({ queryKey: playlistKeys.detail(playlistId) }),
      ]),
  });
}

/** Rename a playlist, optimistically patching the detail cache. */
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

/** Remove a track from a playlist, optimistically dropping the row. */
export function useRemoveTrackFromPlaylist(playlistId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => removeTrackFromPlaylist(playlistId, trackId),
    onMutate: async (trackId) => {
      await queryClient.cancelQueries({ queryKey: playlistKeys.detail(playlistId) });
      const previous = queryClient.getQueryData<{ tracks: { id: string }[] }>(
        playlistKeys.detail(playlistId),
      );
      if (previous) {
        queryClient.setQueryData(playlistKeys.detail(playlistId), {
          ...previous,
          tracks: previous.tracks.filter((t) => t.id !== trackId),
        });
      }
      return { previous };
    },
    onError: (_err, _trackId, context) => {
      if (context?.previous) {
        queryClient.setQueryData(playlistKeys.detail(playlistId), context.previous);
      }
      Alert.alert('Remove failed', 'Could not remove the track. Please try again.');
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: playlistKeys.detail(playlistId) }),
        queryClient.invalidateQueries({ queryKey: playlistKeys.list }),
      ]),
  });
}
