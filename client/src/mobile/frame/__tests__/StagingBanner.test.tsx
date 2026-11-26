import { render } from '@testing-library/react-native';
import { Text } from 'react-native';
import { renderRouter, screen } from 'expo-router/testing-library';

import { installAuthClient } from '@shared/auth';
import { ThemeProvider } from '@shared/ui';
import { authClientOver } from '@testing/fakeAuthClient';

import { RootLayout, StagingBanner } from '@/mobile';

const STAGING_URL = 'https://ijyjoyxhwmbmriwzazbx.supabase.co';
const PROD_URL = 'https://ellvexundmgvbbfqbzau.supabase.co';
const BANNER_TEXT = 'Staging. Everything here is wiped every night. Use altune.duckdns.org.';

function renderBannerBuiltFor(supabaseUrl: string | undefined) {
  return render(
    <ThemeProvider>
      <StagingBanner supabaseUrl={supabaseUrl} />
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
    const { queryByText } = renderBannerBuiltFor(undefined);

    expect(queryByText(BANNER_TEXT)).toBeNull();
  });
});

describe('the root layout on the staging build', () => {
  const originalUrl = process.env.EXPO_PUBLIC_SUPABASE_URL;

  afterEach(() => {
    process.env.EXPO_PUBLIC_SUPABASE_URL = originalUrl;
  });

  it('shows the banner above the auth gate even while the gate shows a screen of its own', async () => {
    process.env.EXPO_PUBLIC_SUPABASE_URL = STAGING_URL;
    installAuthClient(
      authClientOver({
        getSession: () => new Promise(() => undefined),
        onAuthStateChange: () => ({ data: { subscription: { unsubscribe: () => undefined } } }),
      }),
    );

    renderRouter({ _layout: RootLayout, index: () => <Text>home</Text> }, { initialUrl: '/' });

    expect(await screen.findByText(BANNER_TEXT)).toBeTruthy();
    expect(screen.getByTestId('auth-splash')).toBeTruthy();
    const tree = JSON.stringify(screen.toJSON());
    expect(tree.indexOf(BANNER_TEXT)).toBeLessThan(tree.indexOf('auth-splash'));
  });
});
