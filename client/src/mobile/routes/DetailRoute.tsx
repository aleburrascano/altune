import { Redirect, useLocalSearchParams, useSegments } from 'expo-router';
import type { ReactElement } from 'react';

import { readDetailHandoff, tabRootFromSegments, useNavigator } from '@shared/navigation';
import { DetailScreen } from '@features/detail';

export function DetailRoute(): ReactElement {
  const params = useLocalSearchParams<{ handoff?: string }>();
  const segments = useSegments();
  const navigator = useNavigator();
  const handoff = readDetailHandoff(params.handoff);
  if (handoff === null) return <Redirect href="/discover" />;
  return (
    <DetailScreen handoff={handoff} tabRoot={tabRootFromSegments(segments)} navigator={navigator} />
  );
}
