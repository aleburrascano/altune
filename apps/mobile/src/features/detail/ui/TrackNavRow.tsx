import { type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

import { sharedStyles } from './styles';

export type TrackNavRowNav = {
  overline: string;
  title: string;
  imageUri: string | null;
  imageRadius: number;
};

export type TrackNavRowInteraction = {
  onPress: () => void;
  disabled?: boolean;
  testID?: string;
  accessibilityLabel: string;
  accessibilityHint?: string;
};

export type TrackNavRowProps = {
  nav: TrackNavRowNav;
  interaction: TrackNavRowInteraction;
};

function pressableStyle(borderColor: string) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.navRow,
    { borderBottomColor: borderColor },
    pressed ? sharedStyles.pressed : null,
  ];
}

export function TrackNavRow({ nav, interaction }: TrackNavRowProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable {...interaction} accessibilityRole="link" style={pressableStyle(theme.color.border)}>
      <Artwork uri={nav.imageUri} size={36} radius={nav.imageRadius} />
      <NavRowText overline={nav.overline} title={nav.title} />
      <ChevronRight size={16} color={theme.color.textTertiary} />
    </Pressable>
  );
}

const overlineTextProps = { variant: 'overline', tone: 'tertiary' } as const;
const titleTextProps = { variant: 'body', numberOfLines: 1 } as const;

function NavRowText({ overline, title }: { overline: string; title: string }): ReactElement {
  return (
    <View style={styles.navText}>
      <Text {...overlineTextProps}>{overline}</Text>
      <Text {...titleTextProps}>{title}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  navRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    minHeight: 56,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  navText: { flex: 1 },
});
