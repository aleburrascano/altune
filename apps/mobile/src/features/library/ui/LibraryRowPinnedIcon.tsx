import type { ReactElement } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { PinnedStatus } from '@shared/offline/pinnedStore';
import { useTheme } from '@shared/ui';

import { pinnedStatusDisplay } from '../pinnedStatusDisplay';

export function LibraryRowPinnedIcon({
  trackId,
  status,
}: {
  trackId: TrackId;
  status: PinnedStatus | undefined;
}): ReactElement | null {
  const theme = useTheme();
  const { icon } = pinnedStatusDisplay(status);
  if (icon === null) return null;
  const Glyph = icon.glyph;
  return (
    <Glyph testID={`${icon.testIdPrefix}-${trackId}`} size={14} color={theme.color[icon.color]} />
  );
}
