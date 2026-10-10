import { QueryClientProvider, useQuery } from '@tanstack/react-query';
import { act, render, screen } from '@testing-library/react-native';
import { Text } from 'react-native';

import { ApiError, NetworkError } from '@shared/failure/errors';
import { createQueryClient } from '@shared/query/createQueryClient';
import { RETRY_BACKOFF_CAP_MS } from '@shared/query/retryDelay';

const FIRST_TRY_PLUS_FIVE_RETRIES = 6;
const REQUESTS_FOR_A_TRANSIENT_FAILURE = FIRST_TRY_PLUS_FIVE_RETRIES;
const REQUESTS_FOR_A_CLIENT_ERROR = 1;
const STATUS_SERVICE_UNAVAILABLE = 503;
const STATUS_NOT_FOUND = 404;
const STATUS_TOO_MANY_REQUESTS = 429;
const MORE_TICKS_THAN_RETRIES = 20;

let serverAnswer: Error = new Error('unset');
let requestsMade = 0;

async function askTheServer(): Promise<never> {
  requestsMade += 1;
  throw serverAnswer;
}

function RetryProbe() {
  const query = useQuery({ queryKey: ['retry-probe'], queryFn: askTheServer });
  return <Text>{`probe ${query.status}`}</Text>;
}

async function runRetriesUntilTheErrorShows() {
  for (let tick = 0; tick < MORE_TICKS_THAN_RETRIES; tick += 1) {
    if (screen.queryByText('probe error') !== null) return;
    await act(async () => {
      await jest.advanceTimersByTimeAsync(RETRY_BACKOFF_CAP_MS);
    });
  }
}

async function requestsMadeForAProbeAnswering(failure: Error): Promise<number> {
  serverAnswer = failure;
  requestsMade = 0;

  const { unmount } = render(
    <QueryClientProvider client={createQueryClient()}>
      <RetryProbe />
    </QueryClientProvider>,
  );
  await runRetriesUntilTheErrorShows();

  expect(screen.getByText('probe error')).toBeTruthy();
  unmount();
  return requestsMade;
}

beforeAll(() => {
  jest.useFakeTimers();
});

afterAll(() => {
  jest.useRealTimers();
});

describe('the app query client the root layout uses retries transient failures and nothing else', () => {
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
