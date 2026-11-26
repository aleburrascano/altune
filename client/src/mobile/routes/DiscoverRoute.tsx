import type { ReactElement } from 'react';

import { useNavigator, useScreenFocusEffect } from '@shared/navigation';
import { dismissKeyboard } from '@shared/ui';
import { DiscoverScreen } from '@features/discovery';

export function DiscoverRoute(): ReactElement {
  const navigator = useNavigator();
  return (
    <DiscoverScreen
      navigator={navigator}
      useFocus={useScreenFocusEffect}
      dismissKeyboard={dismissKeyboard}
    />
  );
}
