import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Pause, Play } from 'lucide-react-native';

import { Row, Text, radius, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { FavoriteButton } from '@shared/favorites';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { pressedStyle } from './pressedStyle';
import { resultSecondaryLine } from '../resultSecondaryLine';
import { usePreviewPlayback } from '../hooks/usePreviewPlayback';

export type DiscoverRowProps = {
  result: DiscoveryResult;
  position: number;
  onPress: (result: DiscoveryResult, position: number) => void;
};

export const ART_SIZE = 56;

export function DiscoverRow({ result, position, onPress }: DiscoverRowProps): ReactElement {
  const testId = `discover-row-${result.kind}-${position}`;
  const isArtist = result.kind === 'artist';
  const secondary = resultSecondaryLine(result);
  const a11yLabel = `${result.title}${secondary ? `, ${secondary}` : ''}`;
  const theme = useTheme();
  const preview = usePreviewPlayback(result);

  return (
    <Pressable
      testID={testId}
      onPress={() => onPress(result, position)}
      accessibilityRole="button"
      accessibilityLabel={a11yLabel}
      style={({ pressed }) => [
        styles.row,
        pressedStyle(pressed),
        pressed ? { backgroundColor: theme.color.surface1 } : null,
      ]}
    >
      <Row
        leading={
          <Artwork
            uri={result.image_url}
            size={ART_SIZE}
            radius={isArtist ? radius.full : radius.md}
            accessibilityLabel={result.title}
          />
        }
        trailing={
          <View style={styles.trailing}>
            {result.favorite_key != null ? (
              <FavoriteButton
                testID={`discover-favorite-${position}`}
                target={{
                  kind: result.kind,
                  favorite_key: result.favorite_key,
                  title: result.title,
                  subtitle: result.subtitle ?? '',
                  image_url: result.image_url ?? undefined,
                }}
              />
            ) : null}
            {preview.hasPreview ? (
              <View style={[styles.previewWrap, { backgroundColor: theme.color.surface2 }]}>
                <IconButton
                  testID={`discover-preview-${position}`}
                  icon={preview.isPlaying ? Pause : Play}
                  size={18}
                  onPress={preview.togglePreview}
                  accessibilityLabel={preview.isPlaying ? 'Pause preview' : 'Play preview'}
                />
              </View>
            ) : null}
          </View>
        }
      >
        <Text variant="bodyStrong" numberOfLines={1}>
          {result.title}
        </Text>
        <Text variant="label" tone="secondary" numberOfLines={1} style={styles.secondary}>
          {secondary}
        </Text>
      </Row>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: {
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.xs,
    borderRadius: radius.md,
  },
  secondary: { marginTop: spacing.xs },
  trailing: {
    flexDirection: 'row' as const,
    alignItems: 'center' as const,
    gap: spacing.xs,
  },
  previewWrap: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center' as const,
    justifyContent: 'center' as const,
    overflow: 'hidden' as const,
  },
});
