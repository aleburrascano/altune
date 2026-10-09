import type { ReactElement, ReactNode } from 'react';

import { Text } from '@shared/ui/primitives/Text';

type FieldErrorProps = {
  testID: string;
  children: ReactNode;
};

export function FieldError({ testID, children }: FieldErrorProps): ReactElement {
  return (
    <Text testID={testID} variant="caption" tone="danger">
      {children}
    </Text>
  );
}
