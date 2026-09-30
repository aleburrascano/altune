import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { View } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';

import { recordFailureShown } from '../../telemetry/userTelemetry';
import type { Theme } from '../theme/theme';
import { radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';
import { Text } from './Text';

export type BannerTone = 'danger' | 'info';

type BannerBase = {
  children: ReactNode;
  style?: StyleProp<ViewStyle>;
  testID?: string;
};

export type BannerProps =
  (BannerBase & { tone?: 'info' }) | (BannerBase & { tone: 'danger'; surface: string });

const MAX_FAILURE_MESSAGE = 200;

function edgeColor(theme: Theme, tone: BannerTone): string {
  switch (tone) {
    case 'danger':
      return theme.color.danger;
    case 'info':
      return theme.color.accent;
  }
}

function useRecordDangerFailure(props: BannerProps): void {
  const surface = props.tone === 'danger' ? props.surface : null;
  const { children } = props;
  const message = typeof children === 'string' ? children.slice(0, MAX_FAILURE_MESSAGE) : '';
  useEffect(() => {
    if (surface !== null) recordFailureShown({ surface, message });
  }, [surface, message]);
}

function bannerBoxStyle(theme: Theme, tone: BannerTone): ViewStyle {
  return {
    backgroundColor: theme.color.surface1,
    borderLeftWidth: 3,
    borderLeftColor: edgeColor(theme, tone),
    borderRadius: radius.md,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.md,
  };
}

function BannerContent({ children }: { children: ReactNode }): ReactNode {
  if (typeof children !== 'string') return children;
  return (
    <Text variant="label" tone="secondary">
      {children}
    </Text>
  );
}

export function Banner(props: BannerProps) {
  const { children, tone = 'info', style, testID } = props;
  const theme = useTheme();
  useRecordDangerFailure(props);
  return (
    <View testID={testID} style={[bannerBoxStyle(theme, tone), style]}>
      <BannerContent>{children}</BannerContent>
    </View>
  );
}
