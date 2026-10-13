import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ModalFrame } from '@shared/ui/primitives/ModalFrame';
import { pressedStyle } from '@shared/ui/primitives/pressedStyle';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { radius, spacing, sheetGrabber, sheetTopRadius } from '@shared/ui/theme/tokens';

export type ActionSheetOption = {
  label: string;
  onPress: () => void;
  tone?: 'default' | 'danger';
  testID?: string;
};

type ActionSheetProps = {
  visible: boolean;
  title?: string | undefined;
  subtitle?: string | undefined;
  options: ActionSheetOption[];
  onClose: () => void;
  testID?: string | undefined;
};

export function ActionSheet({
  visible,
  title,
  subtitle,
  options,
  onClose,
  testID,
}: ActionSheetProps): ReactElement {
  const theme = useTheme();

  return (
    <ModalFrame
      testID={testID}
      visible={visible}
      variant="sheet"
      onClose={onClose}
      closeAction="ui.action_sheet.close"
    >
      <View style={[styles.sheet, { backgroundColor: theme.color.surface1 }]}>
        <View style={[styles.handle, { backgroundColor: theme.color.border }]} />
        {title != null ? (
          <Text variant="title" numberOfLines={2} style={styles.title}>
            {title}
          </Text>
        ) : null}
        {subtitle != null ? (
          <Text variant="caption" tone="secondary" numberOfLines={1} style={styles.subtitle}>
            {subtitle}
          </Text>
        ) : null}
        <View style={styles.options}>
          {options.map((opt) => (
            <Pressable
              key={opt.label}
              testID={opt.testID}
              onPress={() => {
                opt.onPress();
                onClose();
              }}
              accessibilityRole="button"
              accessibilityLabel={opt.label}
              style={({ pressed }) => [
                styles.option,
                { borderBottomColor: theme.color.border },
                pressedStyle(pressed),
              ]}
            >
              <Text variant="body" {...(opt.tone === 'danger' ? { tone: 'danger' } : {})}>
                {opt.label}
              </Text>
            </Pressable>
          ))}
        </View>
        <Pressable
          onPress={onClose}
          accessibilityRole="button"
          accessibilityLabel="Cancel"
          style={({ pressed }) => [
            styles.cancelBtn,
            { backgroundColor: theme.color.surface2 },
            pressedStyle(pressed),
          ]}
        >
          <Text variant="bodyStrong">Cancel</Text>
        </Pressable>
      </View>
    </ModalFrame>
  );
}

const styles = StyleSheet.create({
  sheet: {
    borderTopLeftRadius: sheetTopRadius,
    borderTopRightRadius: sheetTopRadius,
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing['3xl'],
    paddingTop: spacing.md,
  },
  handle: {
    ...sheetGrabber,
    alignSelf: 'center',
    marginBottom: spacing.lg,
  },
  title: { marginBottom: spacing.xs },
  subtitle: { marginBottom: spacing.md },
  options: { marginBottom: spacing.md },
  option: {
    paddingVertical: spacing.lg,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  cancelBtn: {
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    marginTop: spacing.sm,
  },
});
