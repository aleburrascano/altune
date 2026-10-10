import 'react-native-gesture-handler/jestSetup';
import { act, render } from '@testing-library/react-native';
import type { ComponentType, ReactNode } from 'react';

import { DEFAULT_KILL_SWITCH_URL, KILL_SWITCH_POLL_MS } from '@shared/killSwitch/killSwitchPoll';

const { __http } = require('@testing/doubles/fetch.js');

const SLOW_ROOT_LAYOUT_LOAD_TIMEOUT_MS = 60_000;
const SWITCH_DOCUMENT = `GET ${new URL(DEFAULT_KILL_SWITCH_URL).pathname}`;

type StackProps = { children?: ReactNode };

jest.mock('expo-router', () => {
  const { View } = require('react-native');
  function Stack({ children }: StackProps) {
    return <View>{children}</View>;
  }
  Stack.Screen = function StackScreen() {
    return null;
  };
  return {
    Stack,
    Redirect: () => null,
    useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
    useSegments: () => [],
  };
});

jest.mock('expo-font', () => ({ useFonts: () => [true, null] }));

let RootLayout: ComponentType;
let pollsStartedByTheImport: unknown[] = [];

function killSwitchPollsStartedBy(setIntervalSpy: jest.SpyInstance): unknown[] {
  return setIntervalSpy.mock.calls.flatMap((call: unknown[], index: number) =>
    call[1] === KILL_SWITCH_POLL_MS ? [setIntervalSpy.mock.results[index]?.value] : [],
  );
}

beforeAll(() => {
  const setIntervalSpy = jest.spyOn(global, 'setInterval');
  RootLayout = require('@/app/_layout').default;
  pollsStartedByTheImport = killSwitchPollsStartedBy(setIntervalSpy);
}, SLOW_ROOT_LAYOUT_LOAD_TIMEOUT_MS);

describe('the root layout runs the kill-switch poll for as long as it is mounted', () => {
  it('starts no poll when the module is only imported', () => {
    expect(pollsStartedByTheImport).toHaveLength(0);
  });

  it('starts the poll on mount and stops it on unmount, with real timers', async () => {
    __http.reply(SWITCH_DOCUMENT, { status: 200, json: {} });
    const setIntervalSpy = jest.spyOn(global, 'setInterval');
    const clearIntervalSpy = jest.spyOn(global, 'clearInterval');

    const { unmount } = render(<RootLayout />);
    await act(async () => undefined);
    const polls = killSwitchPollsStartedBy(setIntervalSpy);
    expect(polls).toHaveLength(1);
    expect(__http.countFor(SWITCH_DOCUMENT)).toBe(1);

    unmount();

    expect(clearIntervalSpy).toHaveBeenCalledWith(polls[0]);
  });
});
