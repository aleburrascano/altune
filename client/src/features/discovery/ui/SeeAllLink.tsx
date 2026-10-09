import type { ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Text, pressedStyle, spacing, useTheme } from '@shared/ui';

import type { DiscoveryKind } from '@shared/api-client/discovery';

type SeeAllLinkProps = {
  kind: DiscoveryKind;
  title: string;
  onSeeAll: (kind: DiscoveryKind) => void;
};

function seeAllPressableProps({ kind, title, onSeeAll }: SeeAllLinkProps) {
  return {
    testID: `discover-see-all-${kind}`,
    onPress: () => onSeeAll(kind),
    accessibilityRole: 'button' as const,
    accessibilityLabel: `See all ${title.toLowerCase()}`,
    hitSlop: 8,
    style: ({ pressed }: { pressed: boolean }) => [styles.seeAll, pressedStyle(pressed)],
  };
}

function SeeAllChevron(): ReactElement {
  const theme = useTheme();
  return <ChevronRight size={16} color={theme.color.accent} />;
}

function SeeAllLabel({ title }: { title: string }): ReactElement {
  return (
    <>
      <Text variant="label" tone="accent">
        See all {title.toLowerCase()}
      </Text>
      <SeeAllChevron />
    </>
  );
}

export function SeeAllLink(props: SeeAllLinkProps): ReactElement {
  return (
    <Pressable {...seeAllPressableProps(props)}>
      <SeeAllLabel title={props.title} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  seeAll: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.md,
    alignSelf: 'flex-start',
    minHeight: 44,
  },
});
