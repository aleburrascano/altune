import { useLocalSearchParams, useSegments } from 'expo-router';
import type { ReactElement } from 'react';

import { useNavigator } from '@shared/navigation';
import { FeaturingScreen } from '@features/library';

export function FeaturingRoute(): ReactElement {
  const params = useLocalSearchParams<Record<string, string | string[]>>();
  const segments = useSegments();
  return <FeaturingScreen params={params} segments={segments} navigator={useNavigator()} />;
}
