import 'react-native-gesture-handler/jestSetup';
import { act, render, screen } from '@testing-library/react-native';
import type { ComponentType, ReactNode } from 'react';

type StackProps = { children?: ReactNode };

jest.mock('expo-router', () => {
  const { Text } = require('react-native');
  const { useSettingsPort: useSettingsPortUnderTest } = require('@features/settings/ports/SettingsPortProvider');
  function RouteChild() {
    const port = useSettingsPortUnderTest();
    return <Text>{typeof port === 'object' ? 'settings port resolved' : 'no port'}</Text>;
  }
  function Stack({ children }: StackProps) {
    return (
      <>
        <RouteChild />
        {children}
      </>
    );
  }
  Stack.Screen = function StackScreen() {
    return null;
  };
  return {
    Stack,
    Redirect: () => null,
    useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
    usePathname: () => '/',
    useSegments: () => ['(auth)'],
  };
});

jest.mock('expo-font', () => ({ useFonts: () => [true, null] }));

let RootLayout: ComponentType;

beforeAll(() => {
  RootLayout = require('@/app/_layout').default;
}, 60_000);

describe('the root layout provides the feature ports to its routes', () => {
  it('resolves a feature port hook for a route child with no test-provided ports', async () => {
    render(<RootLayout />);
    await act(async () => undefined);

    expect(await screen.findByText('settings port resolved')).toBeTruthy();
  });
});
