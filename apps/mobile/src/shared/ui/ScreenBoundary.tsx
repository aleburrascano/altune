import { Component, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from './primitives/Button';
import { Text } from './primitives/Text';
import { spacing } from './theme/tokens';
import { useTheme } from './theme/useTheme';

// AIDEV-NOTE: The single sanctioned class component in the app. React provides
// no functional API for catching render errors — an error boundary MUST
// implement getDerivedStateFromError / componentDidCatch on a class. The
// "no class components" rule (rn-coding-style.md) yields here by necessity.
// Keep the themed fallback a functional child so it can still use useTheme().

type ScreenBoundaryProps = { children: ReactNode };
type ScreenBoundaryState = { error: Error | null };

function ScreenErrorFallback({ onRetry }: { onRetry: () => void }): ReactNode {
  const theme = useTheme();
  return (
    <View testID="screen-error" style={[styles.container, { backgroundColor: theme.color.canvas }]}>
      <Text variant="displayL" style={styles.title}>
        Something went wrong
      </Text>
      <Text variant="body" tone="secondary" style={styles.body}>
        This screen hit an unexpected error. You can try again.
      </Text>
      <Button testID="screen-error-retry" label="Try again" onPress={onRetry} />
    </View>
  );
}

/**
 * ScreenBoundary — one failure seam per tab stack.
 *
 * Catches render-time errors in the screens it wraps, logs them (structured
 * logging is TBD per architecture.md Observability), and shows a themed
 * fallback with a retry that resets the boundary. A crash degrades to one tab
 * instead of taking down the whole app.
 */
export class ScreenBoundary extends Component<ScreenBoundaryProps, ScreenBoundaryState> {
  state: ScreenBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ScreenBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error): void {
    // Don't swallow it — surface the crash until a structured logger lands.
    console.error('[ScreenBoundary] render error', error);
  }

  private reset = (): void => {
    this.setState({ error: null });
  };

  render(): ReactNode {
    if (this.state.error !== null) {
      return <ScreenErrorFallback onRetry={this.reset} />;
    }
    return this.props.children;
  }
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
    gap: spacing.md,
  },
  title: { textAlign: 'center' },
  body: { textAlign: 'center' },
});
