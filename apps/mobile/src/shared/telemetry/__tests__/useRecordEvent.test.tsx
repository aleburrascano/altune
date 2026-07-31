import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { useRecordEvent } from '../useRecordEvent';
import { recordEvent, type DiscoveryEvent } from '../recordEvent';

jest.mock('../recordEvent', () => ({ recordEvent: jest.fn() }));

const mockRecordEvent = recordEvent as jest.Mock;

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  mockRecordEvent.mockReset();
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warnSpy.mockRestore();
});

describe('useRecordEvent(): best-effort discovery event tracking', () => {
  it('forwards the mutated event to recordEvent and resolves to no data on success', async () => {
    mockRecordEvent.mockResolvedValue(undefined);
    const event: DiscoveryEvent = { type: 'play', search_id: 'search-1' };
    const { result } = renderHook(() => useRecordEvent(), { wrapper });

    act(() => {
      result.current.mutate(event);
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(mockRecordEvent.mock.calls[0]?.[0]).toEqual(event);
    expect(result.current.data).toBeUndefined();
    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('warns with the failed-tracking message and the error, and never rethrows to the caller', async () => {
    const trackingError = new Error('discovery events unreachable');
    mockRecordEvent.mockRejectedValue(trackingError);
    const { result } = renderHook(() => useRecordEvent(), { wrapper });

    expect(() => {
      act(() => {
        result.current.mutate({ type: 'skip' });
      });
    }).not.toThrow();

    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy).toHaveBeenCalledWith('[discovery] event tracking failed', trackingError);
  });
});
