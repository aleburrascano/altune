import React from 'react';
import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import type { BackfillFeaturedResult } from '@shared/api-client/tracks';
import { supabase } from '@shared/auth/supabaseClient';

import { makeWrapper } from '../../../../jest/makeWrapper';
import { httpSettingsPort } from '@features/settings/adapters/http/httpSettingsPort';
import { createMemorySettingsPort } from '@features/settings/__tests__/memorySettingsPort';
import { useBackfillFeatured } from '@features/settings/hooks/useBackfillFeatured';
import { SettingsPortProvider } from '@features/settings/ports/SettingsPortProvider';
import type { SettingsPort } from '@features/settings/ports/settingsPort';
import { useSettingsPort } from '@features/settings/ports/SettingsPortProvider';
import { ApiError, NetworkError, REQUEST_TIMEOUT_MS } from '@shared/api-client';

const { __http, fakeFetch } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const BACKFILL_PATH = '/v1/tracks/featured-backfill';
const SERVER_ERROR_STATUS = 500;

const donePage: BackfillFeaturedResult = {
  scanned: 3,
  updated: 1,
  truncated: false,
  nextOffset: 0,
};
const firstPage: BackfillFeaturedResult = {
  scanned: 2,
  updated: 2,
  truncated: true,
  nextOffset: 2,
};

function wireOf(page: BackfillFeaturedResult) {
  return {
    scanned: page.scanned,
    updated: page.updated,
    truncated: page.truncated,
    next_offset: page.nextOffset,
  };
}

function serveOverHttp(pages: BackfillFeaturedResult[]): SettingsPort {
  pages.forEach((page) => __http.replyOnce(`POST ${BACKFILL_PATH}`, { json: wireOf(page) }));
  __http.replyOnce(`POST ${BACKFILL_PATH}`, {
    status: SERVER_ERROR_STATUS,
    json: { error: { code: 'internal', message: 'boom' } },
  });
  return httpSettingsPort;
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

afterEach(() => {
  __http.reset();
  global.fetch = fakeFetch;
});

describe.each<[string, (pages: BackfillFeaturedResult[]) => SettingsPort]>([
  ['createMemorySettingsPort', (pages) => createMemorySettingsPort({ pages })],
  ['httpSettingsPort over HTTP', serveOverHttp],
])('%s satisfies the SettingsPort contract', (_name, seededPort) => {
  it('serves one page', async () => {
    const port = seededPort([donePage]);

    expect(await port.backfillFeaturedArtists()).toEqual(donePage);
  });

  it('serves several pages in order until the last is not truncated', async () => {
    const port = seededPort([firstPage, donePage]);

    const first = await port.backfillFeaturedArtists();
    const second = await port.backfillFeaturedArtists(first.nextOffset);

    expect([first, second]).toEqual([firstPage, donePage]);
  });

  it('rejects when the server fails', async () => {
    const port = seededPort([]);

    await expect(port.backfillFeaturedArtists()).rejects.toBeInstanceOf(Error);
  });
});

describe('useBackfillFeatured on the memory fake', () => {
  it('totals every seeded page with no api-client mock', async () => {
    const queryClient = new QueryClient();
    const port = createMemorySettingsPort({ pages: [firstPage, donePage] });
    const Query = makeWrapper(queryClient);
    function wrapper({ children }: { children: React.ReactNode }) {
      return React.createElement(Query, null, SettingsPortProvider({ port, children }));
    }

    const { result } = renderHook(() => useBackfillFeatured(), { wrapper });
    act(() => {
      result.current.mutate();
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toMatchObject({ scanned: 5, updated: 3, truncated: false });
  });
});

describe('httpSettingsPort on the wire', () => {
  it('sends no offset on the first call and the previous next offset on the next', async () => {
    __http.replyOnce(`POST ${BACKFILL_PATH}`, {
      json: { scanned: 2, updated: 2, truncated: true, next_offset: 2 },
    });
    __http.replyOnce(`POST ${BACKFILL_PATH}`, {
      json: { scanned: 3, updated: 1, truncated: false, next_offset: 0 },
    });

    const first = await httpSettingsPort.backfillFeaturedArtists();
    await httpSettingsPort.backfillFeaturedArtists(first.nextOffset);

    expect(__http.requests.map((r: { query: string }) => r.query)).toEqual(['', 'offset=2']);
  });

  it('gives up on a request the server holds open past the client timeout', async () => {
    jest.useFakeTimers();
    try {
      __http.hang(`POST ${BACKFILL_PATH}`);

      const outcome = httpSettingsPort.backfillFeaturedArtists().then(
        () => 'resolved',
        (error: unknown) => error,
      );
      await jest.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS - 1);
      expect(__http.last().signal.aborted).toBe(false);

      await jest.advanceTimersByTimeAsync(1);
      const error = await outcome;

      expect(error).toBeInstanceOf(NetworkError);
      expect((error as NetworkError).failure).toBe('timeout');
      expect(__http.last().signal.aborted).toBe(true);
      expect(__http.countFor(`POST ${BACKFILL_PATH}`)).toBe(1);
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('useBackfillFeatured with no SettingsPortProvider', () => {
  function startBackfill() {
    const hook = renderHook(() => useBackfillFeatured(), {
      wrapper: makeWrapper(new QueryClient()),
    });
    act(() => {
      hook.result.current.mutate();
    });
    return hook;
  }

  it('reports zero counts after one POST when the library has nothing to scan', async () => {
    __http.replyOnce(`POST ${BACKFILL_PATH}`, {
      json: { scanned: 0, updated: 0, truncated: false, next_offset: 0 },
    });

    const hook = startBackfill();

    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(hook.result.current.data).toMatchObject({ scanned: 0, updated: 0, truncated: false });
    expect(__http.countFor(`POST ${BACKFILL_PATH}`)).toBe(1);
    hook.unmount();
  });

  it('fails with the server error when a later page is refused, without data', async () => {
    __http.replyOnce(`POST ${BACKFILL_PATH}`, {
      json: { scanned: 2, updated: 2, truncated: true, next_offset: 2 },
    });
    __http.replyOnce(`POST ${BACKFILL_PATH}`, {
      status: 400,
      json: { error: { code: 'bad_request', message: 'bad offset' } },
    });

    const hook = startBackfill();

    await waitFor(() => expect(hook.result.current.isError).toBe(true));
    expect(hook.result.current.error).toBeInstanceOf(ApiError);
    expect((hook.result.current.error as ApiError).status).toBe(400);
    expect(hook.result.current.data).toBeUndefined();
    const backfillQueries = __http.requests
      .filter((r: { path: string }) => r.path === BACKFILL_PATH)
      .map((r: { query: string }) => r.query);
    expect(backfillQueries).toEqual(['', 'offset=2']);
    hook.unmount();
  });
});

describe('useSettingsPort', () => {
  function providing(port: SettingsPort) {
    return function Wrapper({ children }: { children: React.ReactNode }) {
      return SettingsPortProvider({ port, children });
    };
  }

  it('returns the HTTP adapter when no provider is mounted', () => {
    const { result } = renderHook(() => useSettingsPort());

    expect(result.current).toBe(httpSettingsPort);
  });

  it('returns the port a provider supplies', () => {
    const port = createMemorySettingsPort();

    const { result } = renderHook(() => useSettingsPort(), { wrapper: providing(port) });

    expect(result.current).toBe(port);
  });

  it('returns the innermost port when providers nest', () => {
    const outer = createMemorySettingsPort();
    const inner = createMemorySettingsPort();
    function Nested({ children }: { children: React.ReactNode }) {
      return SettingsPortProvider({
        port: outer,
        children: SettingsPortProvider({ port: inner, children }),
      });
    }

    const { result } = renderHook(() => useSettingsPort(), { wrapper: Nested });

    expect(result.current).toBe(inner);
  });
});
