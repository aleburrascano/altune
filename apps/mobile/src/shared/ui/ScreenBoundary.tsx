import { Component, type ErrorInfo, type ReactNode } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { Button } from './primitives/Button';
import { Text } from './primitives/Text';
import { spacing } from './theme/tokens';
import { useTheme } from './theme/useTheme';

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

export class ScreenBoundary extends Component<ScreenBoundaryProps, ScreenBoundaryState> {
  state: ScreenBoundaryState = { error: null, componentStack: null };

  static getDerivedStateFromError(error: Error): Partial<ScreenBoundaryState> {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
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
