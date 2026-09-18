import type { ReactElement } from 'react';
import { StyleSheet } from 'react-native';

import { useDownloadPhase } from '@shared/acquisition/downloadStore';
import { withFeaturing } from '@shared/lib/featured';
import { Text } from '@shared/ui';

import { LibraryRowFailure } from './LibraryRowFailure';
import { acquisitionProgressLabel, albumSuffix } from './libraryRowLabels';

import type { TrackResponse } from '@shared/api-client/types';

export function LibraryRowDetails({
  track,
  isPlaying,
  retrying,
  onRetry,
}: {
  track: TrackResponse;
  isPlaying: boolean;
  retrying: boolean;
  onRetry: (() => void) | undefined;
}): ReactElement {
  const phase = useDownloadPhase(track.id);
  const progress = acquisitionProgressLabel(phase, track.acquisition_status);
  return (
    <>
      <Text
        variant="bodyStrong"
        numberOfLines={1}
        {...(isPlaying ? { tone: 'accent' as const } : {})}
      >
        {track.title}
      </Text>
      <Text variant="label" tone="secondary" numberOfLines={1} style={styles.subtitle}>
        {withFeaturing(track.artist, track.featured_artists)}
        {albumSuffix(track.album)}
      </Text>
      {progress != null ? (
        <Text
          testID={`library-row-pending-${track.id}`}
          variant="caption"
          tone="tertiary"
          style={styles.pending}
        >
          {progress}
        </Text>
      ) : null}
      {track.acquisition_status === 'failed' ? (
        <LibraryRowFailure track={track} retrying={retrying} onRetry={onRetry} />
      ) : null}
    </>
  );
}

const styles = StyleSheet.create({
  subtitle: { marginTop: 2 },
  pending: { marginTop: 2 },
});
