import { Component, type ErrorInfo, type ReactNode } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

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
type ScreenBoundaryState = { error: Error | null; componentStack: string | null };

function ScreenErrorFallback({
  error,
  componentStack,
  onRetry,
}: {
  error: Error;
  componentStack: string | null;
  onRetry: () => void;
}): ReactNode {
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
      {/* Dev-only: surface the actual error + the component that threw, so a
          crash is diagnosable on-device without digging through Metro logs.
          Stripped in production builds (__DEV__ is false). */}
      {__DEV__ ? (
        <ScrollView style={styles.devBox} contentContainerStyle={styles.devContent}>
          <Text testID="screen-error-detail" variant="label" tone="tertiary" style={styles.devText}>
            {error.message}
          </Text>
          {componentStack !== null ? (
            <Text variant="caption" tone="tertiary" style={styles.devText}>
              {componentStack.trim()}
            </Text>
          ) : null}
        </ScrollView>
      ) : null}
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
  state: ScreenBoundaryState = { error: null, componentStack: null };

  static getDerivedStateFromError(error: Error): Partial<ScreenBoundaryState> {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // Don't swallow it — surface the crash until a structured logger lands.
    console.error('[ScreenBoundary] render error', error, info.componentStack);
    this.setState({ componentStack: info.componentStack ?? null });
  }

  private reset = (): void => {
    this.setState({ error: null, componentStack: null });
  };

  render(): ReactNode {
    if (this.state.error !== null) {
      return (
        <ScreenErrorFallback
          error={this.state.error}
          componentStack={this.state.componentStack}
          onRetry={this.reset}
        />
      );
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
  devBox: { maxHeight: 220, marginTop: spacing.lg, alignSelf: 'stretch' },
  devContent: { gap: spacing.sm },
  devText: { textAlign: 'left' },
});
