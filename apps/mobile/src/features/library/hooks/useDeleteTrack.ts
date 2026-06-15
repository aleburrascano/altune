import { Alert } from 'react-native';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { deleteTrack } from '@shared/api-client/tracks';
import type { ListTracksResponse } from '@shared/api-client/types';

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => deleteTrack(trackId),
    onMutate: async (trackId) => {
      await queryClient.cancelQueries({ queryKey: ['library-home'] });
      const previous = queryClient.getQueryData<ListTracksResponse>(['library-home']);
      if (previous) {
        queryClient.setQueryData<ListTracksResponse>(['library-home'], {
          ...previous,
          items: previous.items.filter((t) => t.id !== trackId),
          total: Math.max(0, previous.total - 1),
        });
      }
      return { previous };
    },
    onError: (_err, _trackId, context) => {
      if (context?.previous) {
        queryClient.setQueryData(['library-home'], context.previous);
      }
      Alert.alert('Delete failed', 'Could not remove the track. Please try again.');
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['library-home'] });
      void queryClient.invalidateQueries({ queryKey: ['library'] });
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
      void queryClient.invalidateQueries({ queryKey: ['playlist'] });
    },
  });
}
