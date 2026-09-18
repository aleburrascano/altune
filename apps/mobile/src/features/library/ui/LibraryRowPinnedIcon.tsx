import type { ReactElement } from 'react';

import { ArrowDownCircle, CircleAlert, CircleCheck } from 'lucide-react-native';

import type { TrackId } from '@shared/api-client/ids';
import type { PinnedStatus } from '@shared/offline/pinnedStore';
import { useTheme } from '@shared/ui';

export function LibraryRowPinnedIcon({
  trackId,
  status,
}: {
  trackId: TrackId;
  status: PinnedStatus | undefined;
}): ReactElement | null {
  const theme = useTheme();
  if (status === 'ready') {
    return (
      <CircleCheck testID={`library-row-offline-${trackId}`} size={14} color={theme.color.accent} />
    );
  }
  if (status === 'downloading' || status === 'queued') {
    return (
      <ArrowDownCircle
        testID={`library-row-offline-pending-${trackId}`}
        size={14}
        color={theme.color.textTertiary}
      />
    );
  }
  if (status === 'failed') {
    return (
      <CircleAlert
        testID={`library-row-offline-failed-${trackId}`}
        size={14}
        color={theme.color.danger}
      />
    );
  }
  return null;
}
