import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { useRecordEvent } from '@shared/telemetry/useRecordEvent';
import { signInTestUser } from '@testing/signInTestUser';

import { applyKillSwitches } from '@shared/killSwitch/killSwitch';
import type { DiscoveryEvent } from '@shared/telemetry/recordEvent';

const TRACKING_FAILED = '[discovery] event tracking failed';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

let warnSpy: jest.SpyInstance;

function trackingWarnings(): unknown[][] {
  return warnSpy.mock.calls.filter(([message]) => message === TRACKING_FAILED);
}

beforeEach(async () => {
  await signInTestUser();
  applyKillSwitches({ telemetry_enabled: true });
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warnSpy.mockRestore();
});

describe('useRecordEvent(): best-effort discovery event tracking', () => {
  it('forwards the mutated event to recordEvent and resolves to no data on success', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });
    const event: DiscoveryEvent = { type: 'play', search_id: 'search-1' };
    const { result } = renderHook(() => useRecordEvent(), { wrapper });

    act(() => {
      result.current.mutate(event);
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(JSON.parse(__http.last().body)).toMatchObject(event);
    expect(result.current.data).toBeUndefined();
    expect(trackingWarnings()).toEqual([]);
  });

  it('warns with the failed-tracking message and the error, and never rethrows to the caller', async () => {
    __http.reply('POST /v1/discovery/events', { status: 500, json: { message: 'unreachable' } });
    const { result } = renderHook(() => useRecordEvent(), { wrapper });

    expect(() => {
      act(() => {
        result.current.mutate({ type: 'skip' });
      });
    }).not.toThrow();

    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(trackingWarnings()).toEqual([
      [TRACKING_FAILED, expect.objectContaining({ status: 500 })],
    ]);
  });
});

describe('useRecordEvent(): kill switch off', () => {
  it('settles quietly when the event is gated, without the failed-tracking warning or a request', async () => {
    applyKillSwitches({ telemetry_enabled: false });
    const { result } = renderHook(() => useRecordEvent(), { wrapper });

    act(() => {
      result.current.mutate({ type: 'play', search_id: 'search-1' });
    });

    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(trackingWarnings()).toEqual([]);
    expect(__http.requests.length).toBe(0);
  });
});
