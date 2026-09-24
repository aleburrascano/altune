import { type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

import { sharedStyles } from './styles';

type TrackNavRowProps = {
  overline: string;
  title: string;
  imageUri: string | null;
  imageRadius: number;
  accessibilityLabel: string;
  accessibilityHint?: string;
  onPress: () => void;
  disabled?: boolean;
  testID?: string;
};

export function TrackNavRow(props: TrackNavRowProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable
      testID={props.testID}
      onPress={props.onPress}
      disabled={props.disabled}
      accessibilityRole="link"
      accessibilityLabel={props.accessibilityLabel}
      accessibilityHint={props.accessibilityHint}
      style={({ pressed }) => [
        styles.navRow,
        { borderBottomColor: theme.color.border },
        pressed ? sharedStyles.pressed : null,
      ]}
    >
      <Artwork uri={props.imageUri} size={36} radius={props.imageRadius} />
      <NavRowText overline={props.overline} title={props.title} />
      <ChevronRight size={16} color={theme.color.textTertiary} />
    </Pressable>
  );
}

function NavRowText({ overline, title }: { overline: string; title: string }): ReactElement {
  return (
    <View style={styles.navText}>
      <Text variant="overline" tone="tertiary">
        {overline}
      </Text>
      <Text variant="body" numberOfLines={1}>
        {title}
      </Text>
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
