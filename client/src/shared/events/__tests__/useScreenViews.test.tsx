import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { applyKillSwitches } from '@shared/killSwitch/killSwitch';
import { useScreenViews } from '@shared/events/useScreenViews';
import { signInTestUser } from '@testing/signInTestUser';

const { __http } = require('@testing/doubles/fetch.js');

let mockSegments: string[] = [];

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));
jest.mock('expo-router', () => ({ useSegments: () => mockSegments }));

const EVENTS = 'POST /v1/discovery/events';
const ONE_EVENT = 1;
const TWO_EVENTS = 2;
const SETTLE_TURNS = 20;

interface WrapperProps {
  children: React.ReactNode;
}

function wrapper({ children }: WrapperProps) {
  const queryClient = new QueryClient();
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

interface SentRequest {
  body?: string;
}

function sentRoutes(): string[] {
  return __http.requests
    .filter((request: SentRequest) => request.body !== undefined)
    .map((request: SentRequest) => JSON.parse(request.body ?? '{}').payload.route);
}

function mount() {
  return renderHook(() => useScreenViews(), { wrapper });
}

async function settle(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < SETTLE_TURNS; i += 1) await Promise.resolve();
  });
}

beforeEach(() => {
  applyKillSwitches({ telemetry_enabled: true });
  __http.reply(EVENTS, { status: 202 });
  mockSegments = ['(tabs)', 'home'];
});

describe('useScreenViews()', () => {
  it('sends one screen_view per route change, with route patterns only', async () => {
    await signInTestUser();
    const { rerender } = mount();
    await waitFor(() => expect(__http.countFor(EVENTS)).toBe(ONE_EVENT));
    expect(sentRoutes()).toEqual(['/(tabs)/home']);
    expect(JSON.parse(__http.last().body).type).toBe('screen_view');

    mockSegments = ['(tabs)', 'library', 'playlist', '[id]'];
    rerender({});
    await waitFor(() => expect(__http.countFor(EVENTS)).toBe(TWO_EVENTS));
    expect(sentRoutes()).toEqual(['/(tabs)/home', '/(tabs)/library/playlist/[id]']);
  });

  it('sends nothing when the same route renders again', async () => {
    await signInTestUser();
    const { rerender } = mount();
    await waitFor(() => expect(__http.countFor(EVENTS)).toBe(ONE_EVENT));

    mockSegments = ['(tabs)', 'home'];
    rerender({});
    await settle();
    expect(__http.countFor(EVENTS)).toBe(ONE_EVENT);
  });

  it('sends nothing while signed out', async () => {
    mount();
    await settle();
    expect(__http.countFor(EVENTS)).toBe(0);
  });

  it('warns instead of throwing when the send fails', async () => {
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    applyKillSwitches({ telemetry_enabled: false });
    await signInTestUser();
    mount();
    await waitFor(() =>
      expect(warnSpy).toHaveBeenCalledWith('[telemetry] recording event failed', expect.anything()),
    );
    warnSpy.mockRestore();
  });
});
