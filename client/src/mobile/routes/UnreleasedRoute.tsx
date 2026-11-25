import { Redirect, useLocalSearchParams } from 'expo-router';
import type { ReactElement } from 'react';

import { readDetailHandoff } from '@shared/navigation';
import { UnreleasedAllScreen } from '@features/detail';

export function UnreleasedRoute(): ReactElement {
  const params = useLocalSearchParams<{ handoff?: string; where?: string }>();
  const handoff = readDetailHandoff(params.handoff);
  if (handoff === null) return <Redirect href="/discover" />;
  return <UnreleasedAllScreen handoff={handoff} where={params.where} />;
}
