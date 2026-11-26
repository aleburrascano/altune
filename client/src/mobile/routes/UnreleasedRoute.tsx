import { Redirect, useLocalSearchParams } from 'expo-router';
import type { ReactElement } from 'react';

import { readDetailHandoff, useNavigator } from '@shared/navigation';
import { UnreleasedAllScreen } from '@features/detail';

export function UnreleasedRoute(): ReactElement {
  const params = useLocalSearchParams<{ handoff?: string; where?: string }>();
  const navigator = useNavigator();
  const handoff = readDetailHandoff(params.handoff);
  if (handoff === null) return <Redirect href="/discover" />;
  return <UnreleasedAllScreen handoff={handoff} navigator={navigator} where={params.where} />;
}
