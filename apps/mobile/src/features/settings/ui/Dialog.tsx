import type { ReactElement, ReactNode } from 'react';
import { KeyboardAvoidingView, Modal, Platform, Pressable, ScrollView, StyleSheet } from 'react-native';

import { radius, spacing, useTheme } from '@shared/ui';

type DialogProps = {
  visible: boolean;
  onClose: () => void;
  testID?: string | undefined;
  children: ReactNode;
};

export function Dialog({ visible, onClose, testID, children }: DialogProps): ReactElement {
  const theme = useTheme();
  return (
    <Modal testID={testID} visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <Pressable
        style={[styles.backdrop, { backgroundColor: theme.color.scrim }]}
        onPress={onClose}
        accessibilityRole="button"
        accessibilityLabel="Close"
      />
      <KeyboardAvoidingView
        style={styles.centering}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        pointerEvents="box-none"
      >
        <ScrollView
          style={styles.scroll}
          contentContainerStyle={styles.scrollContent}
          keyboardShouldPersistTaps="handled"
        >
          <Pressable
            style={[
              styles.card,
              { backgroundColor: theme.color.surface1, borderColor: theme.color.border },
            ]}
          >
            {children}
          </Pressable>
        </ScrollView>
      </KeyboardAvoidingView>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: StyleSheet.absoluteFillObject,
  centering: { flex: 1, justifyContent: 'center' },
  scroll: { flexGrow: 0 },
  scrollContent: { padding: spacing.xl },
  card: { borderRadius: radius.xl, borderWidth: StyleSheet.hairlineWidth, padding: spacing.xl },
});
