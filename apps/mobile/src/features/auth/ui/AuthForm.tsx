import { Link } from 'expo-router';
import type { ComponentProps, ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import { AuthErrorBanner } from './AuthErrorBanner';
import { AuthHeroLayout } from './hero/AuthHeroLayout';
import { OAuthButtons } from './OAuthButtons';

type AuthFormProps = {
  screenTestID: string;
  tagline: string;
  submitLabel: string;
  onSubmit: () => void;
  pending: boolean;
  canSubmit: boolean;
  // Taken from the banner rather than restated: it owns the state shape, and a
  // form that only passes it through must not be able to disagree with it.
  state: ComponentProps<typeof AuthErrorBanner>['state'];
  generic: string;
  linkHref: '/sign-in' | '/sign-up';
  linkTestID: string;
  linkQuestion: string;
  linkAction: string;
  children: ReactNode;
};

export function AuthForm({
  screenTestID,
  tagline,
  submitLabel,
  onSubmit,
  pending,
  canSubmit,
  state,
  generic,
  linkHref,
  linkTestID,
  linkQuestion,
  linkAction,
  children,
}: AuthFormProps): ReactElement {
  return (
    <AuthHeroLayout testID={screenTestID} tagline={tagline} background={false}>
      <View style={styles.form}>
        {children}
        <AuthErrorBanner state={state} generic={generic} />
        <Button
          testID="submit-button"
          label={submitLabel}
          onPress={onSubmit}
          loading={pending}
          disabled={!canSubmit}
        />
        <OAuthButtons />
        <View style={styles.linkWrap}>
          <Link href={linkHref} testID={linkTestID}>
            <Text variant="label" tone="secondary">
              {linkQuestion}{' '}
              <Text variant="label" tone="accent">
                {linkAction}
              </Text>
            </Text>
          </Link>
        </View>
      </View>
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.sm },
  linkWrap: { alignItems: 'center', paddingTop: spacing.sm },
});
