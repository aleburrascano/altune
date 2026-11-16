import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { Pause, Play } from 'lucide-react-native';

import {
  Row,
  Text,
  pressedStyle,
  radius,
  spacing,
  useTheme,
  Artwork,
  IconButton,
  Tappable,
  halved,
} from '@shared/ui';
import { FavoriteButton } from '@features/discovery/view/favorites/FavoriteButton';
import type { DiscoveryResult } from '@shared/catalog';
import type { ResultTapHandler } from '@features/discovery/hooks/results/useResultTap';

import { resultSecondaryLine } from '@features/discovery/domain/resultDisplay';
import { usePreviewPlayback } from '@features/discovery/hooks/results/usePreviewPlayback';
type DiscoverRowProps = {
  result: DiscoveryResult;
  position: number;
  onPress: ResultTapHandler;
};

export const ART_SIZE = 56;

function RowFavorite({
  result,
  position,
}: {
  result: DiscoveryResult;
  position: number;
}): ReactElement | null {
  if (result.favorite_key == null) return null;
  return (
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
  );
}

function RowPreview({
  preview,
  position,
}: {
  preview: ReturnType<typeof usePreviewPlayback>;
  position: number;
}): ReactElement | null {
  const theme = useTheme();
  if (!preview.hasPreview) return null;
  return (
    <View style={[styles.previewWrap, { backgroundColor: theme.color.surface2 }]}>
      <IconButton
        action="discover.preview.toggle"
        testID={`discover-preview-${position}`}
        icon={preview.isPlaying ? Pause : Play}
        size={18}
        onPress={preview.togglePreview}
        accessibilityLabel={preview.isPlaying ? 'Pause preview' : 'Play preview'}
      />
    </View>
  );
}

export function DiscoverRow({ result, position, onPress }: DiscoverRowProps): ReactElement {
  const testId = `discover-row-${result.kind}-${position}`;
  const isArtist = result.kind === 'artist';
  const secondary = resultSecondaryLine(result);
  const a11yLabel = `${result.title}${secondary ? `, ${secondary}` : ''}`;
  const theme = useTheme();
  const preview = usePreviewPlayback(result);

  return (
    <Tappable
      action="discover.row.open"
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
            <RowFavorite result={result} position={position} />
            <RowPreview preview={preview} position={position} />
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
    </Tappable>
  );
}

const PREVIEW_SIZE = 36;

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
    width: PREVIEW_SIZE,
    height: PREVIEW_SIZE,
    borderRadius: halved(PREVIEW_SIZE),
    alignItems: 'center' as const,
    justifyContent: 'center' as const,
    overflow: 'hidden' as const,
  },
});
