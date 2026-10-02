import 'react-native-gesture-handler/jestSetup';
import { render } from '@testing-library/react-native';
import type { ComponentType, ReactNode } from 'react';

import { ThemeProvider } from '@shared/ui/theme';

import { StagingBanner } from '../StagingBanner';

jest.mock('@features/auth/ui/AuthGate', () => ({
  AuthGate: () => {
    const { Text } = require('react-native');
    return <Text testID="gate-own-screen">splash</Text>;
  },
}));

jest.mock('expo-router', () => {
  const { View } = require('react-native');
  const Stack = ({ children }: { children?: ReactNode }) => <View>{children}</View>;
  Stack.Screen = () => null;
  return {
    Stack,
    useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
    useSegments: () => [],
  };
});

jest.mock('@shared/killSwitch/killSwitchPoll', () => ({
  startKillSwitchPolling: () => () => undefined,
}));

jest.mock('expo-font', () => ({ useFonts: () => [true, null] }));

const STAGING_URL = 'https://ijyjoyxhwmbmriwzazbx.supabase.co';
const PROD_URL = 'https://ellvexundmgvbbfqbzau.supabase.co';
const BANNER_TEXT = 'Staging. Everything here is wiped every night. Use altune.duckdns.org.';

const originalUrl = process.env.EXPO_PUBLIC_SUPABASE_URL;

afterEach(() => {
  process.env.EXPO_PUBLIC_SUPABASE_URL = originalUrl;
});

function renderBannerBuiltFor(supabaseUrl: string) {
  process.env.EXPO_PUBLIC_SUPABASE_URL = supabaseUrl;
  return render(
    <ThemeProvider>
      <StagingBanner />
    </ThemeProvider>,
  );
}

describe('StagingBanner', () => {
  it('tells the person this is staging and wiped nightly on the staging build', () => {
    const { getByText } = renderBannerBuiltFor(STAGING_URL);

    expect(getByText(BANNER_TEXT)).toBeTruthy();
  });

  it('renders nothing on the prod build', () => {
    const { queryByText } = renderBannerBuiltFor(PROD_URL);

    expect(queryByText(BANNER_TEXT)).toBeNull();
  });

  it('renders nothing on a build that sets no Supabase URL at all', () => {
    delete process.env.EXPO_PUBLIC_SUPABASE_URL;

    const { queryByText } = render(
      <ThemeProvider>
        <StagingBanner />
      </ThemeProvider>,
    );

    expect(queryByText(BANNER_TEXT)).toBeNull();
  });
});

describe('the root layout on the staging build', () => {
  it('shows the banner above the auth gate even while the gate shows a screen of its own', () => {
    process.env.EXPO_PUBLIC_SUPABASE_URL = STAGING_URL;
    jest.useFakeTimers();
    const RootLayout: ComponentType = require('@/app/_layout').default;

    const { getByText, getByTestId, toJSON, unmount } = render(<RootLayout />);

    expect(getByText(BANNER_TEXT)).toBeTruthy();
    expect(getByTestId('gate-own-screen')).toBeTruthy();
    const tree = JSON.stringify(toJSON());
    expect(tree.indexOf(BANNER_TEXT)).toBeLessThan(tree.indexOf('gate-own-screen'));
    unmount();
    jest.useRealTimers();
  });
});
