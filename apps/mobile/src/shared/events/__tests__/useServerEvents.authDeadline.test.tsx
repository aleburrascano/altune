import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';

import { useServerEvents } from '../useServerEvents';
import { supabase } from '@shared/auth/supabaseClient';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

jest.mock('react-native/Libraries/AppState/AppState', () => ({
  default: {
    currentState: 'active',
    isAvailable: true,
    addEventListener: jest.fn(() => ({ remove: jest.fn() })),
  },
}));

const getSession = supabase.auth.getSession as jest.Mock;

function Harness(): null {
  useServerEvents();
  return null;
}

let renderer: ReactTestRenderer | null = null;

beforeEach(() => {
  jest.useFakeTimers();
  jest.spyOn(Math, 'random').mockReturnValue(0);
  getSession.mockReset();
  getSession.mockReturnValue(new Promise(() => undefined));
});

afterEach(() => {
  act(() => {
    renderer?.unmount();
  });
  renderer = null;
  jest.restoreAllMocks();
  jest.useRealTimers();
});

async function advance(ms: number): Promise<void> {
  await act(async () => {
    jest.advanceTimersByTime(ms);
  });
}

describe('useServerEvents() when the token lookup never settles', () => {
  it('gives up on the lookup at 15000ms and schedules a reconnect that asks for a token again', async () => {
    act(() => {
      renderer = create(
        <QueryClientProvider client={new QueryClient()}>
          <Harness />
        </QueryClientProvider>,
      );
    });
    expect(getSession).toHaveBeenCalledTimes(1);

    await advance(14_999);
    await advance(1_000);
    expect(getSession).toHaveBeenCalledTimes(1);

    await advance(1);
    await advance(1_000);
    expect(getSession).toHaveBeenCalledTimes(2);
  });
});
