import { useEffect, type ReactElement } from 'react';
import {
  StyleSheet,
  View,
  ActivityIndicator,
  Pressable,
  type GestureResponderEvent,
  type PressableProps,
} from 'react-native';
import { Text, spacing, useTheme } from '@shared/ui';
import { recordFailureShownOnce } from '@shared/acquisition/acquisitionTelemetry';
import type { FailedAcquisition, TrackFields } from '@shared/apiClient/trackResponse';
import type { TrackId } from '@shared/apiClient/ids';

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
  const message = track.failure_message ?? 'Acquisition failed';
  useEffect(() => {
    if (!retrying) recordFailureShownOnce(track.id, message);
  }, [track.id, message, retrying]);
  return (
    <View style={styles.failedRow}>
      <Text
        testID={`library-row-failed-${track.id}`}
        variant="caption"
        tone="danger"
        style={styles.failed}
        numberOfLines={1}
      >
        {retrying ? 'Retrying…' : message}
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

const RETRY_HIT_SLOP = 8;

type RetryActionProps = {
  trackId: TrackId;
  title: string;
  retrying: boolean;
  onRetry: () => void;
};

type RetryButtonProps = Omit<RetryActionProps, 'retrying'>;

function RetryingIndicator({ trackId }: Pick<RetryActionProps, 'trackId'>): ReactElement {
  const theme = useTheme();
  return (
    <ActivityIndicator
      testID={`library-row-retrying-${trackId}`}
      size="small"
      color={theme.color.accent}
    />
  );
}

function retryPressHandler(onRetry: () => void): (e: GestureResponderEvent) => void {
  return (e) => {
    e?.stopPropagation?.();
    onRetry();
  };
}

function retryButtonProps({ trackId, title, onRetry }: RetryButtonProps): PressableProps {
  return {
    testID: `library-row-retry-${trackId}`,
    onPress: retryPressHandler(onRetry),
    hitSlop: RETRY_HIT_SLOP,
    accessibilityRole: 'button',
    accessibilityLabel: `Retry acquisition for ${title}`,
  };
}

function RetryButton(props: RetryButtonProps): ReactElement {
  return (
    <Pressable {...retryButtonProps(props)}>
      <Text variant="caption" tone="accent">
        Retry
      </Text>
    </Pressable>
  );
}

function LibraryRowRetryAction({ retrying, ...rest }: RetryActionProps): ReactElement {
  return retrying ? <RetryingIndicator trackId={rest.trackId} /> : <RetryButton {...rest} />;
}
