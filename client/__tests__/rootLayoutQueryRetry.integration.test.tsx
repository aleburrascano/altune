import 'react-native-gesture-handler/jestSetup';
import { useQuery } from '@tanstack/react-query';
import { act, render, screen } from '@testing-library/react-native';
import type { ComponentType, ReactNode } from 'react';
import { Text } from 'react-native';

import { ApiError, NetworkError } from '@shared/failure/errors';
import { RETRY_BACKOFF_CAP_MS } from '@shared/query/retryDelay';

// The first try plus five retries: the app's query client gives a transient failure five more goes.
const REQUESTS_FOR_A_TRANSIENT_FAILURE = 6;
const REQUESTS_FOR_A_CLIENT_ERROR = 1;
const STATUS_SERVICE_UNAVAILABLE = 503;
const STATUS_NOT_FOUND = 404;
const STATUS_TOO_MANY_REQUESTS = 429;
const MORE_TICKS_THAN_RETRIES = 20;
// Loading the whole root layout is slow on a busy machine; keep it out of each test's budget.
const ROOT_LAYOUT_LOAD_TIMEOUT_MS = 60_000;

let RootLayout: ComponentType;

let mockServerAnswer: Error = new Error('unset');
let mockRequestsMade = 0;

async function mockAskTheServer(): Promise<never> {
  mockRequestsMade += 1;
  throw mockServerAnswer;
}

function MockRetryProbe() {
  const query = useQuery({ queryKey: ['retry-probe'], queryFn: mockAskTheServer });
  return <Text>{`probe ${query.status}`}</Text>;
}

jest.mock('@features/auth/ui/AuthGate', () => ({
  AuthGate: () => <MockRetryProbe />,
}));

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
    useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
    useSegments: () => [],
  };
});

jest.mock('@shared/killSwitch/killSwitchPoll', () => ({
  startKillSwitchPolling: () => () => undefined,
}));

jest.mock('expo-font', () => ({ useFonts: () => [true, null] }));

async function runRetriesUntilTheErrorShows() {
  for (let tick = 0; tick < MORE_TICKS_THAN_RETRIES; tick += 1) {
    if (screen.queryByText('probe error') !== null) return;
    await act(async () => {
      await jest.advanceTimersByTimeAsync(RETRY_BACKOFF_CAP_MS);
    });
  }
}

async function requestsMadeForAProbeAnswering(failure: Error): Promise<number> {
  mockServerAnswer = failure;
  mockRequestsMade = 0;

  const { unmount } = render(<RootLayout />);
  await runRetriesUntilTheErrorShows();

  expect(screen.getByText('probe error')).toBeTruthy();
  unmount();
  return mockRequestsMade;
}

beforeAll(() => {
  jest.useFakeTimers();
  RootLayout = require('@/app/_layout').default;
}, ROOT_LAYOUT_LOAD_TIMEOUT_MS);

afterAll(() => {
  jest.useRealTimers();
});

describe('the root layout query client retries transient failures and nothing else', () => {
  it('retries a query that keeps failing with a server fault five times before showing the error', async () => {
    const requests = await requestsMadeForAProbeAnswering(
      new ApiError(STATUS_SERVICE_UNAVAILABLE, 'service unavailable'),
    );

    expect(requests).toBe(REQUESTS_FOR_A_TRANSIENT_FAILURE);
  });

  it('retries a rate-limited query the same way as a server fault', async () => {
    const requests = await requestsMadeForAProbeAnswering(
      new ApiError(STATUS_TOO_MANY_REQUESTS, 'too many requests'),
    );

    expect(requests).toBe(REQUESTS_FOR_A_TRANSIENT_FAILURE);
  });

  it('retries a query whose request never reached the server', async () => {
    const requests = await requestsMadeForAProbeAnswering(
      new NetworkError('transport', 'Network request failed'),
    );

    expect(requests).toBe(REQUESTS_FOR_A_TRANSIENT_FAILURE);
  });

  it('gives up on a 4xx at once instead of retrying a request that cannot succeed', async () => {
    const requests = await requestsMadeForAProbeAnswering(
      new ApiError(STATUS_NOT_FOUND, 'not found'),
    );

    expect(requests).toBe(REQUESTS_FOR_A_CLIENT_ERROR);
  });
});
