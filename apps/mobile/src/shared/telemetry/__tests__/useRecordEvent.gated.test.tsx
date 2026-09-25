import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { useRecordEvent } from '../useRecordEvent';
import { recordEvent } from '../recordEvent';

jest.mock('../recordEvent', () => ({ recordEvent: jest.fn() }));

const mockRecordEvent = recordEvent as jest.Mock;

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function gatedError(): Error {
  const error = new Error('telemetry is switched off');
  error.name = 'TelemetryGatedError';
  return error;
}

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  mockRecordEvent.mockReset();
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warnSpy.mockRestore();
});

describe('useRecordEvent(): kill switch off', () => {
  it('settles quietly when the event is gated, without the failed-tracking warning', async () => {
    mockRecordEvent.mockRejectedValue(gatedError());
    const { result } = renderHook(() => useRecordEvent(), { wrapper });

    act(() => {
      result.current.mutate({ type: 'play', search_id: 'search-1' });
    });

    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(warnSpy).not.toHaveBeenCalled();
  });
});
