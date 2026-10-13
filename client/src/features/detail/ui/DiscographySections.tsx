import type { ReactElement } from 'react';
import { View, StyleSheet } from 'react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import {
  useDiscographyFilter,
  type RecordTypeGroup,
} from '@features/detail/hooks/useDiscographyFilter';
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
      <RecordTypeChips present={present} active={active} onSelect={select} />
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

type ChipProps = { group: RecordTypeGroup; on: boolean; onSelect: (type: string) => void };

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
    action: 'detail.discography.select',
    testID: `detail-discography-${props.group.type}`,
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

type RecordTypeChipsProps = {
  present: readonly RecordTypeGroup[];
  active: RecordTypeGroup;
  onSelect: (type: string) => void;
};

const MIN_GROUPS_FOR_CHIPS = 2;

function chipPropsFor(props: RecordTypeChipsProps, group: RecordTypeGroup): ChipProps {
  return { group, on: group.type === props.active.type, onSelect: props.onSelect };
}

function RecordTypeChips(props: RecordTypeChipsProps): ReactElement | null {
  if (props.present.length < MIN_GROUPS_FOR_CHIPS) return null;
  return (
    <View style={styles.chips}>
      {props.present.map((group) => (
        <Chip key={group.type} {...chipPropsFor(props, group)} />
      ))}
    </View>
  );
}

const DISCOGRAPHY_ROW_MIN_HEIGHT = 36;

const styles = StyleSheet.create({
  chips: { flexDirection: 'row', gap: spacing.sm, marginBottom: spacing.md, flexWrap: 'wrap' },
  chip: {
    minHeight: DISCOGRAPHY_ROW_MIN_HEIGHT,
    paddingHorizontal: spacing.md,
    justifyContent: 'center',
    borderRadius: radius.full,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
