import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text, type TextTone } from '@shared/ui/primitives/Text';
import { spacing, type TypographyVariant } from '@shared/ui/theme';
import { Button } from '@shared/ui/primitives/Button';
import { asyncView } from '@shared/ui/async/asyncView';
import { AsyncSection } from '@shared/ui/async/AsyncSection';

import { sharedStyles } from '@features/detail/ui/detailStyles';
import type { ContentFailure } from '@features/detail/domain/contentStatus';

export function Section({
  label,
  action,
  children,
}: {
  label: string;
  action?: { label: string; onPress: () => void; testID?: string };
  children: ReactNode;
}): ReactElement {
  return (
    <View style={styles.section}>
      <View style={styles.head}>
        <Text variant="overline" tone="tertiary">
          {label.toUpperCase()}
        </Text>
        {action != null ? (
          <Tappable
            action="detail.section.action"
            testID={action.testID}
            onPress={action.onPress}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel={action.label}
            style={({ pressed }) => (pressed ? sharedStyles.pressed : null)}
          >
            <Text variant="caption" tone="accent">
              {action.label}
            </Text>
          </Tappable>
        ) : null}
      </View>
      {children}
    </View>
  );
}

const SECTION_HEADER_MIN_HEIGHT = 20;

const styles = StyleSheet.create({
  section: { marginTop: spacing['2xl'] },
  head: {
    flexDirection: 'row',
    alignItems: 'baseline',
    justifyContent: 'space-between',
    marginBottom: spacing.md,
    minHeight: SECTION_HEADER_MIN_HEIGHT,
  },
});

export interface SectionErrorProps {
  testIDPrefix: string;
  message: string;
  onRetry: () => void;
  failure?: ContentFailure | null;
}

const SETTLED_NOTE = 'Not available right now — try again later.';

export function SectionError({
  testIDPrefix,
  message,
  onRetry,
  failure,
}: SectionErrorProps): ReactElement {
  return (
    <View testID={`${testIDPrefix}-error`} style={sectionErrorStyles.container}>
      <Text variant="body" tone="danger">
        {message}
      </Text>
      {failure === 'settled' ? (
        <Text
          testID={`${testIDPrefix}-settled`}
          variant="caption"
          tone="tertiary"
          style={sectionErrorStyles.settledNote}
        >
          {SETTLED_NOTE}
        </Text>
      ) : (
        <Button
          action="detail.section.retry"
          testID={`${testIDPrefix}-retry`}
          label="Retry"
          onPress={onRetry}
          style={sharedStyles.retryButton}
        />
      )}
    </View>
  );
}

const sectionErrorStyles = StyleSheet.create({
  container: { alignItems: 'center', paddingVertical: spacing.md },
  settledNote: { marginTop: spacing.xs, textAlign: 'center' },
});

export interface AsyncEmptyConfig {
  message: string;
  variant: TypographyVariant;
  tone: TextTone;
}

export interface AsyncListSectionProps {
  isLoading: boolean;
  isError: boolean;
  isEmpty: boolean;
  skeleton: () => ReactNode;
  error: SectionErrorProps;
  empty: AsyncEmptyConfig;
  children: ReactNode;
}

export function AsyncListSection({
  isLoading,
  isError,
  isEmpty,
  skeleton,
  error,
  empty,
  children,
}: AsyncListSectionProps): ReactElement {
  return (
    <AsyncSection
      view={asyncView({ isLoading, isError, isEmpty })}
      skeleton={skeleton}
      error={() => <SectionError {...error} />}
      empty={() => (
        <Text variant={empty.variant} tone={empty.tone} style={asyncListSectionStyles.emptySection}>
          {empty.message}
        </Text>
      )}
    >
      {children}
    </AsyncSection>
  );
}

const asyncListSectionStyles = StyleSheet.create({
  emptySection: { paddingVertical: spacing.md },
});
