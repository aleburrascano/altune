import { useMutation, useQueryClient } from '@tanstack/react-query';

import { deleteTrack } from '@shared/api-client/tracks';

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => deleteTrack(trackId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['library-home'] });
      void queryClient.invalidateQueries({ queryKey: ['library'] });
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
    },
  });
}
