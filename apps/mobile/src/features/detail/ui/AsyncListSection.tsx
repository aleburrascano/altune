import { type ReactElement, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text, type TextTone } from '@shared/ui/primitives/Text';
import { spacing, type TypographyVariant } from '@shared/ui/theme';

import { asyncView } from '@shared/lib/async-view';
import { AsyncSection } from '@shared/ui/AsyncSection';

import { sharedStyles } from './styles';

/** Copy + styling for the empty slot of an {@link AsyncListSection}. */
export interface AsyncEmptyConfig {
  message: string;
  variant: TypographyVariant;
  tone: TextTone;
}

/** Copy, styling and retry affordance for the error slot. */
export interface AsyncErrorConfig extends AsyncEmptyConfig {
  testID?: string;
  retryTestID?: string;
  onRetry: () => void;
}

export interface AsyncListSectionProps {
  isLoading: boolean;
  isError: boolean;
  isEmpty: boolean;
  /** Builds the loading placeholder. Called only while loading. */
  skeleton: () => ReactNode;
  error: AsyncErrorConfig;
  empty: AsyncEmptyConfig;
  /** Rendered when content is ready. */
  children: ReactNode;
}

/**
 * The one loading → error → empty → ready list block for artist detail. It owns
 * the centered "couldn't load" + retry error shape and the padded empty line so
 * the three detail lists (tracks, API discography, explore discography) can no
 * longer drift apart; per-list copy, tone and testIDs stay verbatim via props.
 */
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
      error={() => (
        <View testID={error.testID} style={styles.sectionError}>
          <Text variant={error.variant} tone={error.tone}>
            {error.message}
          </Text>
          <Button
            {...(error.retryTestID != null ? { testID: error.retryTestID } : {})}
            label="Retry"
            onPress={error.onRetry}
            style={sharedStyles.retryButton}
          />
        </View>
      )}
      empty={() => (
        <Text variant={empty.variant} tone={empty.tone} style={styles.emptySection}>
          {empty.message}
        </Text>
      )}
    >
      {children}
    </AsyncSection>
  );
}

const styles = StyleSheet.create({
  sectionError: { paddingVertical: spacing.md, alignItems: 'center' },
  emptySection: { paddingVertical: spacing.md },
});
