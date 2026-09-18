import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable } from 'react-native';

import type { TrackId } from '@shared/api-client/ids';
import { Text, useTheme } from '@shared/ui';

export function LibraryRowRetryAction({
  trackId,
  title,
  retrying,
  onRetry,
}: {
  trackId: TrackId;
  title: string;
  retrying: boolean;
  onRetry: () => void;
}): ReactElement {
  const theme = useTheme();
  if (retrying) {
    return (
      <ActivityIndicator
        testID={`library-row-retrying-${trackId}`}
        size="small"
        color={theme.color.accent}
      />
    );
  }
  return (
    <Pressable
      testID={`library-row-retry-${trackId}`}
      onPress={(e) => {
        e?.stopPropagation?.();
        onRetry();
      }}
      hitSlop={8}
      accessibilityRole="button"
      accessibilityLabel={`Retry acquisition for ${title}`}
    >
      <Text variant="caption" tone="accent">
        Retry
      </Text>
    </Pressable>
  );
}
