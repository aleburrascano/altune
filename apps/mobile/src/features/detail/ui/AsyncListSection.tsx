import { type ReactElement, type ReactNode } from 'react';
import { StyleSheet } from 'react-native';

import { Text, type TextTone } from '@shared/ui/primitives/Text';
import { spacing, type TypographyVariant } from '@shared/ui/theme';

import { asyncView } from '@shared/lib/async-view';
import { AsyncSection } from '@shared/ui/AsyncSection';

import { SectionError, type SectionErrorProps } from './SectionError';

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
