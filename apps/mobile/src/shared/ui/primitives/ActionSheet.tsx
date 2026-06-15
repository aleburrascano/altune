import type { ReactElement } from 'react';
import { Modal, Pressable, StyleSheet, View } from 'react-native';

import { Text } from './Text';
import { useTheme } from '../theme/useTheme';
import { radius, spacing } from '../theme/tokens';

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
    <Modal
      testID={testID}
      visible={visible}
      transparent
      animationType="slide"
      onRequestClose={onClose}
    >
      <Pressable style={styles.backdrop} onPress={onClose}>
        <View />
      </Pressable>
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
              onPress={() => { opt.onPress(); onClose(); }}
              accessibilityRole="button"
              accessibilityLabel={opt.label}
              style={({ pressed }) => [
                styles.option,
                { borderBottomColor: theme.color.border },
                pressed ? styles.pressed : null,
              ]}
            >
              <Text
                variant="body"
                {...(opt.tone === 'danger' ? { tone: 'danger' } : {})}
              >
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
            pressed ? styles.pressed : null,
          ]}
        >
          <Text variant="bodyStrong">Cancel</Text>
        </Pressable>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.5)' },
  sheet: {
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing['3xl'],
    paddingTop: spacing.md,
  },
  handle: {
    width: 36,
    height: 4,
    borderRadius: 2,
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
  pressed: { opacity: 0.7 },
  cancelBtn: {
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    marginTop: spacing.sm,
  },
});
