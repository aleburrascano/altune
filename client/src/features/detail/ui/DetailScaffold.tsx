import { useState, type ReactElement, type ReactNode } from 'react';
import { Animated, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Image } from 'expo-image';
import { LinearGradient } from 'expo-linear-gradient';

import { useWideWebLayout } from '@shared/ui/layout';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import { useTheme, type Theme, spacing } from '@shared/ui/theme';
import { Text } from '@shared/ui/primitives/Text';

import { DetailTopBar, type DetailTopBarProps } from '@features/detail/ui/DetailTopBar';
import { DETAIL_GUTTER, WIDE_LEFT_COLUMN_WIDTH, withAlpha } from '@features/detail/ui/detailStyles';

const BANNER_HEIGHT = 318;
const BAR_HEIGHT = 52;
const SCROLL_TAIL = 140;

export type DetailChrome = {
  title: string;
  secondary?: ReactNode;
  artworkUrl: string | null;
  onBack: () => void;
};

export type DetailScaffoldProps = DetailChrome & {
  actions: ReactNode;
  facts?: ReactNode;
  menuItems?: ContextMenuItem[];
  children: ReactNode;
};

type ChromeOpacityInput = { scrollY: Animated.Value; bannerHeight: number; insetTop: number };

function chromeInterpolation(barTop: number) {
  return {
    inputRange: [barTop - 40, barTop],
    outputRange: [0, 1],
    extrapolate: 'clamp' as const,
  };
}

function chromeOpacityFor(input: ChromeOpacityInput): Animated.AnimatedInterpolation<number> {
  const barTop = input.bannerHeight - BAR_HEIGHT - input.insetTop;
  return input.scrollY.interpolate(chromeInterpolation(barTop));
}

type ScaffoldChrome = {
  bannerHeight: number;
  insetTop: number;
  chromeOpacity: Animated.AnimatedInterpolation<number>;
  scrollY: Animated.Value;
};

function useScaffoldChrome(): ScaffoldChrome {
  const insets = useSafeAreaInsets();
  const [scrollY] = useState(() => new Animated.Value(0));
  const bannerHeight = BANNER_HEIGHT + insets.top;
  const chromeOpacity = chromeOpacityFor({ scrollY, bannerHeight, insetTop: insets.top });
  return { bannerHeight, insetTop: insets.top, chromeOpacity, scrollY };
}

function ScaffoldRoot({ children }: { children: ReactNode }): ReactElement {
  const canvas = useTheme().color.canvas;
  return (
    <View testID="detail-header" style={[styles.root, { backgroundColor: canvas }]}>
      {children}
    </View>
  );
}

function scrollHandler(scrollY: Animated.Value) {
  return Animated.event([{ nativeEvent: { contentOffset: { y: scrollY } } }], {
    useNativeDriver: true,
  });
}

function scrollViewProps(scrollY: Animated.Value) {
  return {
    showsVerticalScrollIndicator: false,
    scrollEventThrottle: 16,
    contentContainerStyle: styles.content,
    onScroll: scrollHandler(scrollY),
  };
}

type ScrollingContentProps = DetailScaffoldProps & { scrollY: Animated.Value; height: number };

function WideScrollingBody(props: ScrollingContentProps): ReactElement {
  return (
    <DetailBodyLayout
      hero={<DetailHeroBanner {...props} />}
      actions={props.actions}
      facts={props.facts}
    >
      {props.children}
    </DetailBodyLayout>
  );
}

function CompactScrollingBody(props: ScrollingContentProps): ReactElement {
  return (
    <>
      <DetailHeroBanner {...props} />
      <DetailBodyLayout actions={props.actions} facts={props.facts}>
        {props.children}
      </DetailBodyLayout>
    </>
  );
}

function ScrollingContent(props: ScrollingContentProps): ReactElement {
  const wide = useWideWebLayout();
  return (
    <Animated.ScrollView {...scrollViewProps(props.scrollY)}>
      {wide ? <WideScrollingBody {...props} /> : <CompactScrollingBody {...props} />}
    </Animated.ScrollView>
  );
}

type ChromeFillProps = { height: number; opacity: Animated.AnimatedInterpolation<number> };

function chromeColorStyle(theme: Theme) {
  return { backgroundColor: theme.color.canvas, borderBottomColor: theme.color.border };
}

function chromeMetricStyle(props: ChromeFillProps) {
  return { height: props.height, opacity: props.opacity };
}

function ChromeFill(props: ChromeFillProps): ReactElement {
  const theme = useTheme();
  return (
    <Animated.View
      pointerEvents="none"
      style={[styles.barFill, chromeColorStyle(theme), chromeMetricStyle(props)]}
    />
  );
}

function topBarProps(props: DetailScaffoldProps, chrome: ScaffoldChrome): DetailTopBarProps {
  return {
    title: props.title,
    onBack: props.onBack,
    menuItems: props.menuItems,
    top: chrome.insetTop,
    height: BAR_HEIGHT,
    chromeOpacity: chrome.chromeOpacity,
  };
}

export function DetailScaffold(props: DetailScaffoldProps): ReactElement {
  const chrome = useScaffoldChrome();
  return (
    <ScaffoldRoot>
      <ScrollingContent {...props} scrollY={chrome.scrollY} height={chrome.bannerHeight} />
      <ChromeFill height={chrome.insetTop + BAR_HEIGHT} opacity={chrome.chromeOpacity} />
      <DetailTopBar {...topBarProps(props, chrome)} />
    </ScaffoldRoot>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  content: { paddingBottom: SCROLL_TAIL },
  barFill: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
});

export type DetailBodyLayoutProps = {
  hero?: ReactNode;
  actions: ReactNode;
  facts?: ReactNode;
  children: ReactNode;
};

type CompactBodyProps = Omit<DetailBodyLayoutProps, 'hero'>;

function CompactBody(props: CompactBodyProps): ReactElement {
  return (
    <View testID="detail-body" style={bodyLayoutStyles.body}>
      {props.actions}
      {props.facts}
      {props.children}
    </View>
  );
}

type WideLeftColumnProps = Pick<DetailBodyLayoutProps, 'hero' | 'actions'>;

function WideLeftColumn({ hero, actions }: WideLeftColumnProps): ReactElement {
  return (
    <View testID="detail-body-left" style={bodyLayoutStyles.left}>
      {hero}
      {actions}
    </View>
  );
}

type WideRightColumnProps = { facts: ReactNode; children: ReactNode };

function WideRightColumn({ facts, children }: WideRightColumnProps): ReactElement {
  return (
    <View testID="detail-body-right" style={bodyLayoutStyles.right}>
      {facts}
      {children}
    </View>
  );
}

function WideBody(props: DetailBodyLayoutProps): ReactElement {
  return (
    <View testID="detail-body-wide" style={bodyLayoutStyles.wideBody}>
      <WideLeftColumn hero={props.hero} actions={props.actions} />
      <WideRightColumn facts={props.facts}>{props.children}</WideRightColumn>
    </View>
  );
}

export function DetailBodyLayout(props: DetailBodyLayoutProps): ReactElement {
  const wide = useWideWebLayout();
  if (wide && props.hero != null) return <WideBody {...props} />;
  return (
    <CompactBody actions={props.actions} facts={props.facts}>
      {props.children}
    </CompactBody>
  );
}

const bodyLayoutStyles = StyleSheet.create({
  body: { paddingHorizontal: DETAIL_GUTTER },
  wideBody: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.xl,
    paddingHorizontal: DETAIL_GUTTER,
  },
  left: { width: WIDE_LEFT_COLUMN_WIDTH, flexShrink: 0 },
  right: { flex: 1, minWidth: 0 },
});

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
    testID: 'detail-artwork',
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
    <View style={heroBannerStyles.bannerText}>
      <Text testID="detail-banner-title" variant="editorial" numberOfLines={2}>
        {title}
      </Text>
      {secondary != null ? <View style={heroBannerStyles.secondary}>{secondary}</View> : null}
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
    <View style={[heroBannerStyles.banner, { height: props.height }]}>
      <BannerImage title={props.title} artworkUrl={props.artworkUrl} />
      <BannerGradient canvas={canvas} />
      <BannerText title={props.title} secondary={props.secondary} />
    </View>
  );
}

const heroBannerStyles = StyleSheet.create({
  banner: { width: '100%', justifyContent: 'flex-end' },
  bannerText: { paddingHorizontal: DETAIL_GUTTER, paddingBottom: DETAIL_GUTTER },
  secondary: { marginTop: spacing.xs, alignSelf: 'flex-start' },
});
