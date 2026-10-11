import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight } from 'lucide-react-native';

import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, spacing, useTheme } from '@shared/ui/theme';

import { sharedStyles } from '@features/detail/ui/detailStyles';

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

function headerPressableProps(props: CollapsibleSectionHeaderProps, onToggle: () => void) {
  return {
    action: 'detail.section.toggle',
    testID: props.testID,
    onPress: onToggle,
    accessibilityRole: 'button' as const,
    accessibilityLabel: props.accessibilityLabel,
  };
}

function pressedHeaderStyle({ pressed }: { pressed: boolean }) {
  return [styles.header, pressed ? sharedStyles.pressed : null];
}

type InteractiveHeaderProps = CollapsibleSectionHeaderProps & { onToggle: () => void };

function InteractiveHeader(props: InteractiveHeaderProps): ReactElement {
  return (
    <Tappable {...headerPressableProps(props, props.onToggle)} style={pressedHeaderStyle}>
      <HeaderContent label={props.label} expanded={props.expanded} />
    </Tappable>
  );
}

export function CollapsibleSectionHeader(props: CollapsibleSectionHeaderProps): ReactElement {
  if (props.onToggle == null) {
    return <StaticHeader label={props.label} />;
  }
  return <InteractiveHeader {...props} onToggle={props.onToggle} />;
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
