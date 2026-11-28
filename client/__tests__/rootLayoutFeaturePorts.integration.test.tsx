import 'react-native-gesture-handler/jestSetup';
import { act, renderRouter, screen } from 'expo-router/testing-library';

import { transientRetryOptions } from '@shared/query';
import { SettingsScreen } from '@features/settings';

import RootLayout from '@/app/_layout';

function RouteChild() {
  return <SettingsScreen retryOptions={transientRetryOptions} />;
}

describe('the root layout provides the feature ports to its routes', () => {
  it('resolves a feature port hook for a route child with no test-provided ports', async () => {
    renderRouter({ _layout: RootLayout, '(auth)/index': RouteChild });
    await act(async () => undefined);

    expect(await screen.findByTestId('settings-backfill-featured')).toBeTruthy();
  }, 60_000);
});
