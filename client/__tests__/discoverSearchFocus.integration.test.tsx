import { Slot, Tabs, router } from 'expo-router';
import { Text, TextInput } from 'react-native';
import { act, renderRouter, screen } from 'expo-router/testing-library';

import { DiscoverRoute } from '@/mobile';
import { useKeyboardShortcuts } from '@shared/ui';
import { makeIdlePlayback } from '@testing/factories';
import {
  createPlaybackWrapper,
  createTestQueryClient,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

const WINDOW_EVENTS = new Map<string, (event: unknown) => void>();

const fakeWindow = {
  addEventListener: (type: string, listener: (event: unknown) => void) => {
    WINDOW_EVENTS.set(type, listener);
  },
  removeEventListener: (type: string) => {
    WINDOW_EVENTS.delete(type);
  },
} as unknown as Window;

function pressSlash(): void {
  WINDOW_EVENTS.get('keydown')?.({
    key: '/',
    shiftKey: false,
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    target: null,
    preventDefault: () => undefined,
  });
}

function Shortcuts(): null {
  useKeyboardShortcuts(makeIdlePlayback(), fakeWindow);
  return null;
}

function Root() {
  return (
    <>
      <Shortcuts />
      <Slot />
    </>
  );
}

function TabsLayout() {
  return <Tabs screenOptions={{ headerShown: false }} />;
}

const ROUTES = {
  _layout: Root,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/discover/index': DiscoverRoute,
  '(tabs)/library/index': () => <Text>elsewhere-screen</Text>,
};

async function openAt(url: string) {
  const wrapper = createPlaybackWrapper(createTestQueryClient({ mutations: true }));
  const rendered = renderRouter(ROUTES, { initialUrl: url, wrapper });
  await act(async () => {});
  return rendered;
}

const searchInputFocus = TextInput.prototype.focus as jest.Mock;

beforeEach(async () => {
  WINDOW_EVENTS.clear();
  searchInputFocus.mockClear();
  await signInTestUser();
  __http.replyAll({ status: 200, json: { items: [], total: 0 } });
});

describe('the "/" shortcut and the Discover search box', () => {
  it('asks Discover to open and then focuses its search box after a visit elsewhere', async () => {
    const { getPathname } = await openAt('/discover');
    await act(async () => {
      router.push('/library');
    });
    expect(await screen.findByText('elsewhere-screen')).toBeTruthy();
    expect(
      screen.getByTestId('discover-screen-body', { includeHiddenElements: true }),
    ).toBeTruthy();

    pressSlash();
    await act(async () => {});

    expect(getPathname()).toBe('/discover');
    expect(searchInputFocus).toHaveBeenCalledTimes(1);
  });
});
