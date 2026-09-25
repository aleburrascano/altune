import { memo, type ReactElement } from 'react';

import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { usePinnedStatus } from '../hooks/useLibraryOffline';
import { LibraryRowDetails } from './LibraryRowDetails';
import { LibraryRowPlayback } from './LibraryRowPlayback';
import { LibraryRowSelection } from './LibraryRowSelection';
import { libraryRowAccessibilityLabel } from './libraryRowLabels';

import type { TrackResponse } from '@shared/api-client/types';

type LibraryRowProps = {
  track: TrackResponse;
  onPlay?: () => void;
  onPress: () => void;
  onMore: (anchor: MenuAnchor) => void;
  onRetry?: (() => void) | undefined;
  retrying?: boolean;
  isPlaying?: boolean;
  selectable?: { selected: boolean; onToggle: () => void } | undefined;
  onLongPress?: (() => void) | undefined;
};

function LibraryRowImpl({
  track,
  onPlay,
  onPress,
  onMore,
  onRetry,
  retrying,
  isPlaying,
  selectable,
  onLongPress,
}: LibraryRowProps): ReactElement {
  const pinned = usePinnedStatus(track.id);
  const isRetrying = retrying === true;
  const a11yLabel = libraryRowAccessibilityLabel({
    track,
    retrying: isRetrying,
    canRetry: onRetry != null,
    pinned,
  });
  const details = (
    <LibraryRowDetails
      track={track}
      isPlaying={isPlaying === true}
      retrying={isRetrying}
      onRetry={onRetry}
    />
  );

  if (selectable != null) {
    return (
      <LibraryRowSelection
        track={track}
        a11yLabel={a11yLabel}
        selected={selectable.selected}
        onToggle={selectable.onToggle}
        onLongPress={onLongPress}
      >
        {details}
      </LibraryRowSelection>
    );
  }

  return (
    <LibraryRowPlayback
      track={track}
      a11yLabel={a11yLabel}
      pinned={pinned}
      onPlay={onPlay}
      onPress={onPress}
      onMore={onMore}
      onLongPress={onLongPress}
    >
      {details}
    </LibraryRowPlayback>
  );
}

export const LibraryRow = memo(LibraryRowImpl, (prev, next) => {
  const a = prev.track;
  const b = next.track;
  return (
    a.id === b.id &&
    a.title === b.title &&
    a.artist === b.artist &&
    a.album === b.album &&
    a.artwork_url === b.artwork_url &&
    a.duration_seconds === b.duration_seconds &&
    a.acquisition_status === b.acquisition_status &&
    a.failure_reason === b.failure_reason &&
    a.failure_message === b.failure_message &&
    prev.retrying === next.retrying &&
    prev.isPlaying === next.isPlaying &&
    prev.selectable?.selected === next.selectable?.selected &&
    (prev.selectable == null) === (next.selectable == null) &&
    (prev.onLongPress == null) === (next.onLongPress == null) &&
    (prev.onPlay == null) === (next.onPlay == null) &&
    (prev.onRetry == null) === (next.onRetry == null)
  );
});
