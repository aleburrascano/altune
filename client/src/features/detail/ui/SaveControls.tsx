import type { ReactElement, ReactNode } from 'react';
import { ActivityIndicator, Pressable, StyleSheet } from 'react-native';
import { Ban, Check, Plus, RotateCw } from 'lucide-react-native';

import { useTheme, minInteractiveHeight, radius, spacing, type Theme } from '@shared/ui/theme';
import { Text } from '@shared/ui/primitives/Text';

import {
  type SaveControlState,
  type SaveState,
  saveControlInteractive,
  saveControlLabel,
  saveControlState,
  saveControlText,
  saveDisplayState,
} from '@/features/detail/domain/saveTrack';
import { sharedStyles } from '@features/detail/ui/detailStyles';
import type { OwnedTrack } from '@features/detail/domain/ownedTrack';
import { useResolvedOwnedTrack, type TrackIdentity } from '@features/detail/hooks/useOwnedTrack';

type SaveGlyphProps = {
  state: SaveControlState;
  addSize: number;
};

const RESULT_GLYPHS = {
  ready: { Icon: Check, size: 18, tone: 'success' },
  failed: { Icon: RotateCw, size: 17, tone: 'danger' },
  rejected: { Icon: Ban, size: 17, tone: 'danger' },
} as const;

function SaveGlyph({ state, addSize }: SaveGlyphProps): ReactElement {
  const { color } = useTheme();
  if (state === 'saving') return <ActivityIndicator size="small" color={color.accent} />;
  if (state === 'add') return <Plus size={addSize} color={color.accent} />;
  const { Icon, size, tone } = RESULT_GLYPHS[state];
  return <Icon size={size} color={color[tone]} />;
}

export type SavePillShellProps = {
  onPress: () => void;
  disabled: boolean;
  interactive: boolean;
  accessibilityLabel: string;
  accessibilityState?: { disabled?: boolean; busy?: boolean };
  testID?: string;
  children: ReactNode;
};

function pillStyle(theme: Theme, interactive: boolean) {
  return ({ pressed }: { pressed: boolean }) => [
    savePillShellStyles.savePill,
    { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
    pressed && interactive ? sharedStyles.pressed : null,
  ];
}

function shellA11y(props: SavePillShellProps) {
  return {
    accessibilityRole: 'button' as const,
    accessibilityLabel: props.accessibilityLabel,
    accessibilityState: props.accessibilityState,
  };
}

function shellProps(props: SavePillShellProps, theme: Theme) {
  return {
    testID: props.testID,
    onPress: props.onPress,
    disabled: props.disabled,
    style: pillStyle(theme, props.interactive),
    ...shellA11y(props),
  };
}

export function SavePillShell(props: SavePillShellProps): ReactElement {
  const theme = useTheme();
  return <Pressable {...shellProps(props, theme)}>{props.children}</Pressable>;
}

const savePillShellStyles = StyleSheet.create({
  savePill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.lg,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.full,
    flexShrink: 0,
  },
});

const SIZE = 40;

export function TrackSaveControl({
  owned,
  onPress,
  title,
  artist,
  savingInBatch = false,
  testID,
}: {
  owned: OwnedTrack | null;
  onPress: () => void;
  title: string;
  artist?: string | null;
  savingInBatch?: boolean;
  testID?: string;
}): ReactElement {
  const theme = useTheme();
  const identity: TrackIdentity | undefined = artist != null ? { title, artist } : undefined;
  const resolved = useResolvedOwnedTrack(owned, identity);

  const ownState: SaveControlState = saveControlState(resolved);
  const effective: SaveControlState = savingInBatch && ownState === 'add' ? 'saving' : ownState;
  const interactive = !savingInBatch && saveControlInteractive(effective);

  return (
    <Pressable
      testID={testID}
      onPress={(e) => {
        e.stopPropagation();
        if (!interactive) return;
        onPress();
      }}
      disabled={!interactive}
      accessibilityRole="button"
      accessibilityLabel={saveControlLabel(effective, title)}
      hitSlop={8}
      style={({ pressed }) => [
        trackSaveControlStyles.base,
        effective === 'add' ? { borderWidth: 1.5, borderColor: theme.color.border } : null,
        effective === 'ready' ? { backgroundColor: `${theme.color.success}28` } : null,
        effective === 'failed' ? { backgroundColor: `${theme.color.danger}28` } : null,
        pressed && interactive ? sharedStyles.pressed : null,
      ]}
    >
      <SaveGlyph state={effective} addSize={20} />
    </Pressable>
  );
}

const trackSaveControlStyles = StyleSheet.create({
  base: {
    width: SIZE,
    height: SIZE,
    borderRadius: radius.full,
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
  },
});

type TrackSavePillProps = { save: SaveState; onSave: () => void; title: string };

function savePillA11y({ save, title }: TrackSavePillProps) {
  const interactive = saveControlInteractive(save);
  return {
    disabled: !interactive,
    accessibilityLabel: saveControlLabel(saveDisplayState(save), title),
    accessibilityState: { disabled: !interactive, busy: save === 'saving' },
  };
}

function savePillShellProps(props: TrackSavePillProps) {
  return {
    testID: 'detail-save',
    onPress: props.onSave,
    interactive: saveControlInteractive(props.save),
    ...savePillA11y(props),
  };
}

function saveTextProps(save: SaveState) {
  return {
    variant: 'label' as const,
    tone: save === 'ready' ? 'success' : 'primary',
  } as const;
}

export function TrackSavePill(props: TrackSavePillProps): ReactElement {
  const display = saveDisplayState(props.save);
  return (
    <SavePillShell {...savePillShellProps(props)}>
      <SaveGlyph state={display} addSize={18} />
      <Text {...saveTextProps(props.save)}>{saveControlText(display)}</Text>
    </SavePillShell>
  );
}

type SaveAllPillProps = { unownedCount: number; saving: boolean; onSave: () => void };

function saveAllShellProps(props: SaveAllPillProps) {
  return {
    testID: 'detail-save-all',
    onPress: props.onSave,
    disabled: props.saving,
    interactive: !props.saving,
    accessibilityLabel: `Save ${props.unownedCount} tracks to your library`,
    accessibilityState: { disabled: props.saving },
  };
}

export function SaveAllPill(props: SaveAllPillProps): ReactElement | null {
  const theme = useTheme();
  if (props.unownedCount === 0) return null;
  return (
    <SavePillShell {...saveAllShellProps(props)}>
      <Plus size={18} color={theme.color.accent} />
      <Text variant="label">{props.saving ? 'Saving…' : `Save ${props.unownedCount}`}</Text>
    </SavePillShell>
  );
}
