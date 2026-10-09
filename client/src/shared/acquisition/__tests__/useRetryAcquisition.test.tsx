import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { ApiError } from '@shared/failure/errors';

import { createMemoryAcquisitionPort } from '@shared/acquisition/__tests__/memoryAcquisitionPort';
import { AcquisitionPortProvider } from '@shared/acquisition/ports/AcquisitionPortProvider';
import { useSharedRetryAcquisition } from '@shared/acquisition/useRetryAcquisition';

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

const NOT_FOUND_STATUS = 404;

function renderRetry(trackIds: readonly string[]) {
  const memory = createMemoryAcquisitionPort({ trackIds });
  const onError = jest.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <AcquisitionPortProvider port={memory.port}>{children}</AcquisitionPortProvider>
    </QueryClientProvider>
  );
  const hook = renderHook(
    () =>
      useSharedRetryAcquisition({
        action: 'retry',
        entryPoint: undefined,
        onMutate: () => ({}),
        onError,
      }),
    { wrapper },
  );
  return { ...hook, memory, onError };
}

let fetchSpy: jest.Mock;
const realFetch = global.fetch;

beforeEach(() => {
  fetchSpy = jest.fn();
  global.fetch = fetchSpy as unknown as typeof fetch;
});

afterEach(() => {
  global.fetch = realFetch;
});

describe('useSharedRetryAcquisition over the AcquisitionPort', () => {
  it('records the retry on the fake without any fetch call', async () => {
    const { result, memory } = renderRetry(['t1']);

    await act(async () => {
      await result.current.mutateAsync(asTrackId('t1'));
    });

    expect(memory.retried).toEqual(['t1']);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('reaches onError with a 404 when the track is not seeded', async () => {
    const { result, memory, onError } = renderRetry([]);

    act(() => {
      result.current.mutate(asTrackId('t1'));
    });

    await waitFor(() => expect(onError).toHaveBeenCalledTimes(1));
    const [error] = onError.mock.calls[0] as [ApiError];
    expect(error.status).toBe(NOT_FOUND_STATUS);
    expect(memory.retried).toEqual([]);
  });
});
