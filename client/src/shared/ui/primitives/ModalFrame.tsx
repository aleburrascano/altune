import type { ComponentProps, ReactElement, ReactNode } from 'react';
import { KeyboardAvoidingView, Modal, ScrollView, StyleSheet, View } from 'react-native';

import { keyboardAvoidingBehavior } from '@shared/ui/keyboard/keyboard';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { radius, spacing } from '@shared/ui/theme/tokens';
import { useTheme } from '@shared/ui/theme/useTheme';

type ModalVariant = 'center' | 'sheet' | 'popover';

type ModalFrameProps = {
  visible: boolean;
  onClose: () => void;
  closeAction: string;
  closeLabel?: string;
  variant?: ModalVariant;
  testID?: string | undefined;
  children: ReactNode;
};

export function ModalFrame(props: ModalFrameProps): ReactElement {
  const variant = props.variant ?? 'center';
  return (
    <Modal {...modalProps(props, variant)}>
      <ModalBackdrop {...backdropProps(props, variant)} />
      <ModalBody {...props} variant={variant} />
    </Modal>
  );
}

function modalProps(props: ModalFrameProps, variant: ModalVariant): ComponentProps<typeof Modal> {
  return {
    testID: props.testID,
    visible: props.visible,
    onRequestClose: props.onClose,
    transparent: true,
    animationType: ANIMATION[variant],
  };
}

type ModalBackdropProps = { onClose: () => void; action: string; label: string; isDimmed: boolean };

function backdropProps(props: ModalFrameProps, variant: ModalVariant): ModalBackdropProps {
  return {
    onClose: props.onClose,
    action: props.closeAction,
    label: props.closeLabel ?? 'Close',
    isDimmed: variant !== 'popover',
  };
}

function ModalBackdrop({ onClose, action, label, isDimmed }: ModalBackdropProps): ReactElement {
  const scrim = useTheme().color.scrim;
  const style = [styles.backdrop, isDimmed ? { backgroundColor: scrim } : null];
  const a11y = { accessibilityRole: 'button', accessibilityLabel: label } as const;
  return <Tappable action={action} style={style} onPress={onClose} {...a11y} />;
}

type ModalBodyProps = { variant: ModalVariant; testID?: string | undefined; children: ReactNode };

type KeyboardProps = ComponentProps<typeof KeyboardAvoidingView>;

function keyboardProps(variant: ModalVariant, testID: string | undefined): KeyboardProps {
  return {
    style: LAYOUT[variant],
    behavior: keyboardAvoidingBehavior,
    pointerEvents: 'box-none',
    testID: testID == null ? undefined : `${testID}-keyboard`,
  };
}

function ModalBody({ variant, testID, children }: ModalBodyProps): ReactElement {
  const content = variant === 'center' ? <CenteredCard>{children}</CenteredCard> : children;
  return <KeyboardAvoidingView {...keyboardProps(variant, testID)}>{content}</KeyboardAvoidingView>;
}

type CenteredCardProps = { children: ReactNode };

function CenteredCard({ children }: CenteredCardProps): ReactElement {
  const theme = useTheme();
  const colors = { backgroundColor: theme.color.surface1, borderColor: theme.color.border };
  return (
    <ScrollView {...SCROLL_PROPS}>
      <View style={[styles.card, colors]}>{children}</View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  backdrop: StyleSheet.absoluteFill,
  centering: { flex: 1, justifyContent: 'center' },
  bottomAligned: { flex: 1, justifyContent: 'flex-end' },
  overlay: { flex: 1 },
  scroll: { flexGrow: 0 },
  scrollContent: { padding: spacing.xl },
  card: { borderRadius: radius.xl, borderWidth: StyleSheet.hairlineWidth, padding: spacing.xl },
});

const ANIMATION = { center: 'fade', sheet: 'slide', popover: 'fade' } as const;

const LAYOUT = {
  center: styles.centering,
  sheet: styles.bottomAligned,
  popover: styles.overlay,
} as const;

const SCROLL_PROPS = {
  style: styles.scroll,
  contentContainerStyle: styles.scrollContent,
  keyboardShouldPersistTaps: 'handled',
} as const;
