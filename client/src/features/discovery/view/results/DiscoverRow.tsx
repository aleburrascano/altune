import type { ComponentProps, ReactElement, ReactNode } from 'react';
import {
  StyleSheet,
  View,
  type PressableStateCallbackType,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
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
import {
  FavoriteButton,
  type FavoriteButtonProps,
} from '@features/discovery/view/favorites/FavoriteButton';
import type { DiscoveryResult, FavoriteKey } from '@shared/catalog';
import type { ResultTapHandler } from '@features/discovery/hooks/results/useResultTap';

import { resultSecondaryLine } from '@features/discovery/domain/resultDisplay';
import {
  usePreviewPlayback,
  type PreviewPlayback,
} from '@features/discovery/hooks/results/usePreviewPlayback';
type DiscoverRowProps = {
  result: DiscoveryResult;
  position: number;
  onPress: ResultTapHandler;
};

type RowItemProps = { item: DiscoveryResult; position: number };
type RowArtworkProps = { item: DiscoveryResult };
type RowTextProps = { title: string; secondary: string };
type RowBodyProps = RowItemProps & { secondary: string };
type RowTapArgs = RowBodyProps & { onPress: ResultTapHandler };
type RowTappableProps = RowTapArgs & { children: ReactNode };
type RowTapProps = Pick<
  ComponentProps<typeof Tappable>,
  'action' | 'testID' | 'onPress' | 'accessibilityRole' | 'accessibilityLabel'
>;
type RowPreviewProps = { preview: PreviewPlayback; position: number };
type ShownPreview = Extract<PreviewPlayback, { hasPreview: true }>;
type FavoriteTarget = FavoriteButtonProps['target'];
type IconButtonProps = ComponentProps<typeof IconButton>;
type PressedRowStyle = (state: PressableStateCallbackType) => StyleProp<ViewStyle>;

export const ART_SIZE = 56;
const PREVIEW_ICON_SIZE = 18;

function favoriteTarget(item: DiscoveryResult, key: FavoriteKey): FavoriteTarget {
  return {
    kind: item.kind,
    favorite_key: key,
    title: item.title,
    subtitle: item.subtitle ?? '',
    image_url: item.image_url ?? undefined,
  };
}

function RowFavorite({ item, position }: RowItemProps): ReactElement | null {
  if (item.favorite_key == null) return null;
  const target = favoriteTarget(item, item.favorite_key);
  return <FavoriteButton testID={`discover-favorite-${position}`} target={target} />;
}

function previewToggleProps(preview: ShownPreview, position: number): IconButtonProps {
  return {
    action: 'discover.preview.toggle',
    testID: `discover-preview-${position}`,
    icon: preview.isPlaying ? Pause : Play,
    size: PREVIEW_ICON_SIZE,
    onPress: preview.togglePreview,
    accessibilityLabel: preview.isPlaying ? 'Pause preview' : 'Play preview',
  };
}

function RowPreview({ preview, position }: RowPreviewProps): ReactElement | null {
  const theme = useTheme();
  if (!preview.hasPreview) return null;
  return (
    <View style={[styles.previewWrap, { backgroundColor: theme.color.surface2 }]}>
      <IconButton {...previewToggleProps(preview, position)} />
    </View>
  );
}

function RowTrailing({ item, position }: RowItemProps): ReactElement {
  const preview = usePreviewPlayback(item);
  return (
    <View style={styles.trailing}>
      <RowFavorite item={item} position={position} />
      <RowPreview preview={preview} position={position} />
    </View>
  );
}

function RowArtwork({ item }: RowArtworkProps): ReactElement {
  const shape = item.kind === 'artist' ? radius.full : radius.md;
  return (
    <Artwork uri={item.image_url} size={ART_SIZE} radius={shape} accessibilityLabel={item.title} />
  );
}

function RowTitle({ title }: Pick<RowTextProps, 'title'>): ReactElement {
  return (
    <Text variant="bodyStrong" numberOfLines={1}>
      {title}
    </Text>
  );
}

function RowSecondary({ secondary }: Pick<RowTextProps, 'secondary'>): ReactElement {
  return (
    <Text variant="label" tone="secondary" numberOfLines={1} style={styles.secondary}>
      {secondary}
    </Text>
  );
}

function RowBody({ item, position, secondary }: RowBodyProps): ReactElement {
  const leading = <RowArtwork item={item} />;
  const trailing = <RowTrailing item={item} position={position} />;
  return (
    <Row leading={leading} trailing={trailing}>
      <RowTitle title={item.title} />
      <RowSecondary secondary={secondary} />
    </Row>
  );
}

function rowTapProps({ item, position, onPress, secondary }: RowTapArgs): RowTapProps {
  return {
    action: 'discover.row.open',
    testID: `discover-row-${item.kind}-${position}`,
    onPress: () => onPress(item, position),
    accessibilityRole: 'button',
    accessibilityLabel: `${item.title}${secondary ? `, ${secondary}` : ''}`,
  };
}

function pressedRowStyle(pressedColor: string): PressedRowStyle {
  return ({ pressed }) => [
    styles.row,
    pressedStyle(pressed),
    pressed ? { backgroundColor: pressedColor } : null,
  ];
}

function RowTappable({ children, ...tap }: RowTappableProps): ReactElement {
  const theme = useTheme();
  return (
    <Tappable {...rowTapProps(tap)} style={pressedRowStyle(theme.color.surface1)}>
      {children}
    </Tappable>
  );
}

export function DiscoverRow({ result: item, position, onPress }: DiscoverRowProps): ReactElement {
  const secondary = resultSecondaryLine(item);
  return (
    <RowTappable item={item} position={position} onPress={onPress} secondary={secondary}>
      <RowBody item={item} position={position} secondary={secondary} />
    </RowTappable>
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
