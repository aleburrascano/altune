import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { describeError } from '@shared/lib/describeError';
import { Button, Text, spacing } from '@shared/ui';

import { classifyLibraryError } from '../state';

interface PlaylistDetailFailureProps {
  error: unknown;
  onRetry: () => void;
  onGoToLibrary: () => void;
}

/**
 * Only a 404/410 means the playlist is really gone; an offline device or a 5xx is
 * transient, so telling either one "not found" is both a lie and a dead end (#1705).
 */
export function PlaylistDetailFailure({
  error,
  onRetry,
  onGoToLibrary,
}: PlaylistDetailFailureProps): ReactElement {
  if (classifyLibraryError(error) === 'not-found') {
    return (
      <View testID="playlist-detail-not-found" style={styles.center}>
        <Text variant="title">Playlist not found</Text>
        <Button label="Go back" onPress={onGoToLibrary} />
      </View>
    );
  }

  const { title, body } = describeError(error);
  return (
    <View testID="playlist-detail-error" style={styles.center}>
      <Text variant="title">{title}</Text>
      <Text variant="label" tone="secondary" style={styles.sub}>
        {body}
      </Text>
      <Button testID="playlist-detail-retry" label="Retry" onPress={onRetry} />
    </View>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.lg },
  sub: { textAlign: 'center', paddingHorizontal: spacing['2xl'] },
});
