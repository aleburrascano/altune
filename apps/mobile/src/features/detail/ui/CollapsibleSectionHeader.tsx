import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, spacing, useTheme } from '@shared/ui/theme';

import { sharedStyles } from './styles';

export type CollapsibleSectionHeaderProps = {
  label: string;
  expanded: boolean;
  onToggle?: () => void;
  testID?: string;
  accessibilityLabel?: string;
};

function HeaderLabel({ label }: { label: string }): ReactElement {
  return (
    <Text variant="label" tone="accent">
      {label}
    </Text>
  );
}

function HeaderChevron({ expanded }: { expanded: boolean }): ReactElement {
  const theme = useTheme();
  return expanded ? (
    <ChevronDown size={18} color={theme.color.accent} />
  ) : (
    <ChevronRight size={18} color={theme.color.accent} />
  );
}

function HeaderContent({ label, expanded }: { label: string; expanded: boolean }): ReactElement {
  return (
    <>
      <HeaderLabel label={label} />
      <HeaderChevron expanded={expanded} />
    </>
  );
}

function StaticHeader({ label }: { label: string }): ReactElement {
  return (
    <View style={styles.header}>
      <HeaderLabel label={label} />
    </View>
  );
}

function headerPressableProps(props: CollapsibleSectionHeaderProps) {
  return {
    testID: props.testID,
    onPress: props.onToggle,
    accessibilityRole: 'button' as const,
    accessibilityLabel: props.accessibilityLabel,
  };
}

function pressedHeaderStyle({ pressed }: { pressed: boolean }) {
  return [styles.header, pressed ? sharedStyles.pressed : null];
}

function InteractiveHeader(props: CollapsibleSectionHeaderProps): ReactElement {
  return (
    <Pressable {...headerPressableProps(props)} style={pressedHeaderStyle}>
      <HeaderContent label={props.label} expanded={props.expanded} />
    </Pressable>
  );
}

export function CollapsibleSectionHeader(props: CollapsibleSectionHeaderProps): ReactElement {
  if (props.onToggle == null) {
    return <StaticHeader label={props.label} />;
  }
  return <InteractiveHeader {...props} />;
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.md,
    minHeight: minInteractiveHeight,
  },
});
