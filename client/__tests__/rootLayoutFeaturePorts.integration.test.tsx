import 'react-native-gesture-handler/jestSetup';
import { Text } from 'react-native';
import { act, renderRouter, screen } from 'expo-router/testing-library';

import { useSettingsPort } from '@testing/fakes/settings/useSettingsPort';

import RootLayout from '@/app/_layout';

function RouteChild() {
  const port = useSettingsPort();
  return <Text>{typeof port === 'object' ? 'settings port resolved' : 'no port'}</Text>;
}

describe('the root layout provides the feature ports to its routes', () => {
  it('resolves a feature port hook for a route child with no test-provided ports', async () => {
    renderRouter({ _layout: RootLayout, '(auth)/index': RouteChild });
    await act(async () => undefined);

    expect(await screen.findByText('settings port resolved')).toBeTruthy();
  }, 60_000);
});
