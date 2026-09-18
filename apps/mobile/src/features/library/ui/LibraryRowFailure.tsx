import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

import { LibraryRowRetryAction } from './LibraryRowRetryAction';

import type { FailedAcquisition, TrackFields } from '@shared/api-client/types';

// Only a failed track carries failure text, so this block cannot be rendered for
// a track that has not failed.
type FailedTrack = TrackFields & FailedAcquisition;

export function LibraryRowFailure({
  track,
  retrying,
  onRetry,
}: {
  track: FailedTrack;
  retrying: boolean;
  onRetry: (() => void) | undefined;
}): ReactElement {
  return (
    <View style={styles.failedRow}>
      <Text
        testID={`library-row-failed-${track.id}`}
        variant="caption"
        tone="danger"
        style={styles.failed}
        numberOfLines={1}
      >
        {retrying ? 'Retrying…' : (track.failure_message ?? 'Acquisition failed')}
      </Text>
      {onRetry != null ? (
        <LibraryRowRetryAction
          trackId={track.id}
          title={track.title}
          retrying={retrying}
          onRetry={onRetry}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  failedRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginTop: 2 },
  failed: { flexShrink: 1 },
});
