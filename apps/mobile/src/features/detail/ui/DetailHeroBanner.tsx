import { type ReactElement, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { Image } from 'expo-image';
import { LinearGradient } from 'expo-linear-gradient';

import { Text } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

import { withAlpha } from './color';
import { DETAIL_GUTTER } from './layout';

const GRADIENT_LOCATIONS = [0, 0.24, 0.54, 0.86, 1] as const;

function bannerGradientColors(canvas: string): readonly [string, string, ...string[]] {
  return [
    withAlpha(canvas, 0.55),
    withAlpha(canvas, 0.1),
    withAlpha(canvas, 0.34),
    withAlpha(canvas, 0.9),
    canvas,
  ];
}

function gradientProps(canvas: string) {
  return {
    colors: bannerGradientColors(canvas),
    locations: GRADIENT_LOCATIONS,
    style: StyleSheet.absoluteFill,
    pointerEvents: 'none' as const,
  };
}

function BannerGradient({ canvas }: { canvas: string }): ReactElement {
  return <LinearGradient {...gradientProps(canvas)} />;
}

type BannerImageProps = { title: string; artworkUrl: string | null };

function imageProps(props: BannerImageProps) {
  return {
    source: props.artworkUrl != null ? { uri: props.artworkUrl } : null,
    style: StyleSheet.absoluteFill,
    contentFit: 'cover' as const,
    transition: 200,
    accessibilityLabel: props.title,
  };
}

function BannerImage(props: BannerImageProps): ReactElement {
  return <Image {...imageProps(props)} />;
}

type BannerTextProps = { title: string; secondary?: ReactNode };

function BannerText({ title, secondary }: BannerTextProps): ReactElement {
  return (
    <View style={styles.bannerText}>
      <Text testID="detail-banner-title" variant="editorial" numberOfLines={2}>
        {title}
      </Text>
      {secondary != null ? <View style={styles.secondary}>{secondary}</View> : null}
    </View>
  );
}

export type DetailHeroBannerProps = {
  title: string;
  secondary?: ReactNode;
  artworkUrl: string | null;
  height: number;
};

export function DetailHeroBanner(props: DetailHeroBannerProps): ReactElement {
  const canvas = useTheme().color.canvas;
  return (
    <View style={[styles.banner, { height: props.height }]}>
      <BannerImage title={props.title} artworkUrl={props.artworkUrl} />
      <BannerGradient canvas={canvas} />
      <BannerText title={props.title} secondary={props.secondary} />
    </View>
  );
}

const styles = StyleSheet.create({
  banner: { width: '100%', justifyContent: 'flex-end' },
  bannerText: { paddingHorizontal: DETAIL_GUTTER, paddingBottom: DETAIL_GUTTER },
  secondary: { marginTop: spacing.xs, alignSelf: 'flex-start' },
});
