import { type ReactElement, type ReactNode } from 'react';
import { StyleSheet } from 'react-native';

import { Text, type TextTone } from '@shared/ui/primitives/Text';
import { spacing, type TypographyVariant } from '@shared/ui/theme';

import { asyncView } from '@shared/lib/async-view';
import { AsyncSection } from '@shared/ui/AsyncSection';

import { SectionError, type SectionErrorProps } from './SectionError';

/** Copy + styling for the empty slot of an {@link AsyncListSection}. */
export interface AsyncEmptyConfig {
  message: string;
  variant: TypographyVariant;
  tone: TextTone;
}

export interface AsyncListSectionProps {
  isLoading: boolean;
  isError: boolean;
  isEmpty: boolean;
  /** Builds the loading placeholder. Called only while loading. */
  skeleton: () => ReactNode;
  /** Copy, testID prefix and retry for the shared {@link SectionError} slot. */
  error: SectionErrorProps;
  empty: AsyncEmptyConfig;
  /** Rendered when content is ready. */
  children: ReactNode;
}

/**
 * The one loading → error → empty → ready list block for artist detail. The
 * error slot is always the shared {@link SectionError}, so the three detail
 * lists (tracks, API discography, explore discography) cannot drift apart in
 * tone or testIDs; per-list copy stays verbatim via props.
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
      error={() => <SectionError {...error} />}
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
  emptySection: { paddingVertical: spacing.md },
});
