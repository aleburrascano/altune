import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import { useDiscographyFilter } from '@features/detail/hooks/useDiscographyFilter';
import { AlbumRail } from '@features/detail/ui/AlbumRail';
import { sharedStyles } from '@features/detail/ui/detailStyles';

export function DiscographySections({
  albums,
  onAlbumPress,
}: {
  albums: DiscoveryResult[];
  onAlbumPress: (album: DiscoveryResult) => void;
}): ReactElement | null {
  const filter = useDiscographyFilter(albums);

  if (filter === null) {
    return null;
  }

  const { present, active, items, capped, hasMore, select, expand } = filter;

  return (
    <View>
      <FilterChips
        groups={present}
        activeType={active.type}
        onSelect={select}
        action="detail.discography.select"
        testIDPrefix="detail-discography"
      />
      <AlbumRail
        items={capped}
        total={items.length}
        hasMore={hasMore}
        typeKey={active.type}
        typeLabel={active.label}
        onAlbumPress={onAlbumPress}
        onSeeAll={expand}
      />
    </View>
  );
}

export type ChipGroup = { type: string; label: string; count: number };

type FilterChipsProps = {
  groups: readonly ChipGroup[];
  activeType: string;
  onSelect: (type: string) => void;
  action: string;
  testIDPrefix: string;
};

type ChipProps = Omit<FilterChipsProps, 'groups' | 'activeType'> & {
  group: ChipGroup;
  on: boolean;
};

function chipStyle(theme: ReturnType<typeof useTheme>, on: boolean) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.chip,
    {
      backgroundColor: on ? theme.color.accent : theme.color.surface2,
      borderColor: on ? 'transparent' : theme.color.border,
    },
    pressed ? sharedStyles.pressed : null,
  ];
}

function chipPressableProps(props: ChipProps, theme: ReturnType<typeof useTheme>) {
  return {
    action: props.action,
    testID: `${props.testIDPrefix}-${props.group.type}`,
    onPress: () => props.onSelect(props.group.type),
    accessibilityRole: 'button' as const,
    accessibilityLabel: `${props.group.label}, ${props.group.count}`,
    accessibilityState: { selected: props.on },
    style: chipStyle(theme, props.on),
  };
}

function Chip(props: ChipProps): ReactElement {
  const theme = useTheme();
  return (
    <Tappable {...chipPressableProps(props, theme)}>
      <Text variant="label" tone={props.on ? 'onAccent' : 'secondary'}>
        {props.group.label} {props.group.count}
      </Text>
    </Tappable>
  );
}

const MIN_GROUPS_FOR_CHIPS = 2;

function chipPropsFor(props: FilterChipsProps, group: ChipGroup): ChipProps {
  const { action, testIDPrefix, onSelect } = props;
  return { group, on: group.type === props.activeType, onSelect, action, testIDPrefix };
}

export function FilterChips(props: FilterChipsProps): ReactElement | null {
  if (props.groups.length < MIN_GROUPS_FOR_CHIPS) return null;
  return (
    <View style={styles.chips}>
      {props.groups.map((group) => (
        <Chip key={group.type} {...chipPropsFor(props, group)} />
      ))}
    </View>
  );
}

const CHIP_MIN_HEIGHT = 36;

const styles = StyleSheet.create({
  chips: { flexDirection: 'row', gap: spacing.sm, marginBottom: spacing.md, flexWrap: 'wrap' },
  chip: {
    minHeight: CHIP_MIN_HEIGHT,
    paddingHorizontal: spacing.md,
    justifyContent: 'center',
    borderRadius: radius.full,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
