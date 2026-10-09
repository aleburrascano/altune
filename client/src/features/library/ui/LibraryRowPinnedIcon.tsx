import { ArrowDownCircle, CircleAlert, CircleCheck, type LucideIcon } from 'lucide-react-native';
import type { ReactElement } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { PinnedStatus } from '@shared/offline/pinnedStore';
import { useTheme } from '@shared/ui';

import { pinnedStatusDisplay } from '@features/library/domain/pinnedStatusDisplay';

type PinnedGlyphKind = 'downloaded' | 'downloading' | 'failed';

export const PINNED_GLYPH: Record<PinnedGlyphKind, LucideIcon> = {
  downloaded: CircleCheck,
  downloading: ArrowDownCircle,
  failed: CircleAlert,
};

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
  const Glyph = PINNED_GLYPH[icon.kind];
  return (
    <Glyph testID={`${icon.testIdPrefix}-${trackId}`} size={14} color={theme.color[icon.color]} />
  );
}
