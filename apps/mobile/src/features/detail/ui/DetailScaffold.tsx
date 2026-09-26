import { useState, type ReactElement, type ReactNode } from 'react';
import { Animated, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useWideWebLayout } from '@shared/ui/layout';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import { useTheme, type Theme } from '@shared/ui/theme';

import { DetailBodyLayout } from './DetailBodyLayout';
import { DetailHeroBanner } from './DetailHeroBanner';
import { DetailTopBar, type DetailTopBarProps } from './DetailTopBar';

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
    <DetailBodyLayout hero={<DetailHeroBanner {...props} />} actions={props.actions} facts={props.facts}>
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
