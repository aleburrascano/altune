import { useMutation, useQueryClient } from '@tanstack/react-query';

import { retryAcquisition } from '@shared/api-client/tracks';

export function useRetryAcquisition() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (trackId: string) => retryAcquisition(trackId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['library-home'] });
      void queryClient.invalidateQueries({ queryKey: ['library'] });
      void queryClient.invalidateQueries({ queryKey: ['playlist'] });
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
    },
  });
}
