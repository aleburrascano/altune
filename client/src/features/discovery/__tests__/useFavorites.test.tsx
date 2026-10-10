import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { useFavorites } from '@features/discovery/hooks/useFavorites';
import type { FavoritesResponse, FavoriteTarget } from '@shared/api-client/favorites';
import { asFavoriteKey } from '@shared/api-client/ids';
import { FavoritesPortProvider } from '@features/discovery/ports/FavoritesPortProvider';
import { createMemoryFavoritesPort } from '@features/discovery/__tests__/memoryFavoritesPort';
import type { FavoritesPort } from '@features/discovery/ports/favoritesPort';
import { discoveryKeys } from '@shared/query/queryKeys';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

let mockedAdd: jest.MockedFunction<FavoritesPort['addFavorite']>;
let mockedList: jest.MockedFunction<FavoritesPort['listFavorites']>;
let mockedRemove: jest.MockedFunction<FavoritesPort['removeFavorite']>;
let port: FavoritesPort;

let alertSpy: jest.SpyInstance;
beforeEach(() => {
  const memory = createMemoryFavoritesPort();
  mockedAdd = jest.fn(memory.addFavorite);
  mockedList = jest.fn(memory.listFavorites);
  mockedRemove = jest.fn(memory.removeFavorite);
  port = { addFavorite: mockedAdd, listFavorites: mockedList, removeFavorite: mockedRemove };
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
});
afterEach(() => {
  alertSpy.mockRestore();
});

const target: FavoriteTarget = {
  kind: 'artist',
  favorite_key: asFavoriteKey('artist-1'),
  title: 'Artist One',
  subtitle: 'Artist',
};

const saved: FavoritesResponse = {
  items: [
    { kind: 'artist', key: asFavoriteKey('artist-1'), title: 'Artist One', subtitle: 'Artist' },
  ],
  total: 1,
};

const addedFavorite = saved.items[0]!;

function newClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <FavoritesPortProvider port={port}>{children}</FavoritesPortProvider>
      </QueryClientProvider>
    );
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function flush(times = 8): Promise<void> {
  for (let i = 0; i < times; i += 1) await Promise.resolve();
}

describe('useFavorites(): optimistic toggle', () => {
  it('prepends the target to the favorites cache mid-flight when it was not saved', async () => {
    const pending = deferred<void>();
    mockedAdd.mockReturnValue(pending.promise.then(() => addedFavorite));
    mockedList.mockResolvedValue({ items: [], total: 0 });
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    expect(result.current.isFavorite(target)).toBe(false);

    act(() => result.current.toggle(target));
    await act(flush);

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual(saved);
    expect(mockedAdd).toHaveBeenCalledWith({
      kind: 'artist',
      title: 'Artist One',
      subtitle: 'Artist',
      image_url: undefined,
    });

    await act(async () => {
      pending.resolve();
      await flush();
    });
  });

  it('removes a saved target mid-flight and calls removeFavorite', async () => {
    const pending = deferred<void>();
    mockedRemove.mockReturnValue(pending.promise);
    mockedList.mockResolvedValue({ items: [], total: 0 });
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, saved);

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    expect(result.current.isFavorite(target)).toBe(true);

    act(() => result.current.toggle(target));
    await act(flush);

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual({ items: [], total: 0 });
    expect(mockedRemove).toHaveBeenCalledTimes(1);
    expect(mockedAdd).not.toHaveBeenCalled();

    await act(async () => {
      pending.resolve();
      await flush();
    });
  });

  it('writes the optimistic entry even against a cold cache (no guard on an empty snapshot)', async () => {
    const pending = deferred<void>();
    mockedAdd.mockReturnValue(pending.promise.then(() => addedFavorite));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual(saved);
    await act(async () => {
      pending.resolve();
      await flush();
    });
  });

  it('on failure rolls back to the snapshot, then refetches the favorites', async () => {
    mockedAdd.mockRejectedValue(new Error('boom'));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    const snapshot = { items: [], total: 0 };
    queryClient.setQueryData(discoveryKeys.favorites, snapshot);
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual(snapshot);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: discoveryKeys.favorites });
  });

  it('alerts when saving a favorite fails, so the rollback is not silent', async () => {
    mockedAdd.mockRejectedValue(new Error('boom'));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(alertSpy).toHaveBeenCalledWith(
      'Update failed',
      'Could not update your favorites. Please try again.',
    );
  });

  it('alerts when removing a favorite fails, so the rollback is not silent', async () => {
    mockedRemove.mockRejectedValue(new Error('boom'));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, saved);

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(alertSpy).toHaveBeenCalledWith(
      'Update failed',
      'Could not update your favorites. Please try again.',
    );
  });

  it('does not cancel an in-flight favorites fetch before writing optimistically', async () => {
    const pending = deferred<void>();
    mockedAdd.mockReturnValue(pending.promise.then(() => addedFavorite));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const cancelSpy = jest.spyOn(queryClient, 'cancelQueries');

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(cancelSpy).not.toHaveBeenCalled();
    await act(async () => {
      pending.resolve();
      await flush();
    });
  });

  it('toggles back on a second tap before re-render instead of duplicating the entry', async () => {
    const pendingAdd = deferred<void>();
    const pendingRemove = deferred<void>();
    mockedAdd.mockReturnValue(pendingAdd.promise.then(() => addedFavorite));
    mockedRemove.mockReturnValue(pendingRemove.promise);
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    await act(async () => {
      result.current.toggle(target);
      result.current.toggle(target);
      await flush();
    });

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual({ items: [], total: 0 });
    expect(mockedAdd).toHaveBeenCalledTimes(1);
    expect(mockedRemove).not.toHaveBeenCalled();

    await act(async () => {
      pendingAdd.resolve();
      await flush();
    });
    expect(mockedRemove).toHaveBeenCalledTimes(1);

    await act(async () => {
      pendingRemove.resolve();
      await flush();
    });
  });
});

type HeldRequest = {
  method: 'PUT' | 'DELETE';
  arrive: () => Promise<void>;
  fail: () => void;
  timeOutAfterApplying: () => Promise<void>;
};

type ServerDouble = {
  port: FavoritesPort;
  held: HeldRequest[];
  maxInFlight: () => number;
  isSaved: () => Promise<boolean>;
};

const tapped: FavoriteTarget = {
  kind: 'track',
  favorite_key: asFavoriteKey('Track One'),
  title: 'Track One',
  subtitle: 'Track',
};

function heldServer(): ServerDouble {
  const memory = createMemoryFavoritesPort();
  const held: HeldRequest[] = [];
  let peak = 0;

  function hold<T>(method: HeldRequest['method'], apply: () => Promise<T>): Promise<T> {
    const reply = deferred<T>();
    function settle(): void {
      held.splice(held.indexOf(request), 1);
    }
    const request: HeldRequest = {
      method,
      arrive: async () => {
        settle();
        reply.resolve(await apply());
      },
      fail: () => {
        settle();
        reply.reject(new Error('network down'));
      },
      timeOutAfterApplying: async () => {
        settle();
        await apply();
        reply.reject(new Error('request timed out'));
      },
    };
    held.push(request);
    peak = Math.max(peak, held.length);
    return reply.promise;
  }

  return {
    held,
    maxInFlight: () => peak,
    isSaved: async () => {
      const { items } = await memory.listFavorites();
      return items.some((f) => f.key === tapped.favorite_key);
    },
    port: {
      listFavorites: memory.listFavorites,
      addFavorite: (ref) => hold('PUT', () => memory.addFavorite(ref)),
      removeFavorite: (ref) => hold('DELETE', () => memory.removeFavorite(ref)),
    },
  };
}

async function settle(): Promise<void> {
  await flush();
  await new Promise((resolve) => setTimeout(resolve, 0));
}

async function deliverNewestFirst(server: ServerDouble): Promise<void> {
  while (server.held.length > 0) {
    await act(async () => {
      await server.held[server.held.length - 1]!.arrive();
      await settle();
    });
  }
}

describe('useFavorites(): rapid toggles of one target', () => {
  it('ends in the last tap on the server and on screen when responses arrive reversed', async () => {
    const server = heldServer();
    port = server.port;
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });

    await act(async () => {
      result.current.toggle(tapped);
      result.current.toggle(tapped);
      await settle();
    });
    await deliverNewestFirst(server);

    expect(await server.isSaved()).toBe(false);
    expect(result.current.isFavorite(tapped)).toBe(false);
    expect(server.maxInFlight()).toBe(1);
  });

  it('collapses taps made while a request is in flight into the latest state', async () => {
    const server = heldServer();
    port = server.port;
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });

    await act(async () => {
      result.current.toggle(tapped);
      result.current.toggle(tapped);
      result.current.toggle(tapped);
      await settle();
    });
    expect(server.held.map((r) => r.method)).toEqual(['PUT']);
    await deliverNewestFirst(server);

    expect(await server.isSaved()).toBe(true);
    expect(result.current.isFavorite(tapped)).toBe(true);
  });

  it('keeps a later tap when the first request fails, and sends it', async () => {
    const server = heldServer();
    port = server.port;
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });

    await act(async () => {
      result.current.toggle(tapped);
      result.current.toggle(tapped);
      result.current.toggle(tapped);
      await settle();
    });
    await act(async () => {
      server.held[0]!.fail();
      await settle();
    });

    expect(result.current.isFavorite(tapped)).toBe(true);
    await deliverNewestFirst(server);

    expect(await server.isSaved()).toBe(true);
    expect(result.current.isFavorite(tapped)).toBe(true);
    expect(server.maxInFlight()).toBe(1);
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('rolls back to the server state and alerts when the latest request fails', async () => {
    const server = heldServer();
    port = server.port;
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });

    await act(async () => {
      result.current.toggle(tapped);
      await settle();
    });
    await deliverNewestFirst(server);
    await act(async () => {
      result.current.toggle(tapped);
      await settle();
    });
    expect(result.current.isFavorite(tapped)).toBe(false);
    await act(async () => {
      server.held[0]!.fail();
      await settle();
    });

    expect(await server.isSaved()).toBe(true);
    expect(result.current.isFavorite(tapped)).toBe(true);
    expect(alertSpy).toHaveBeenCalledWith(
      'Update failed',
      'Could not update your favorites. Please try again.',
    );
  });

  it('leaves the next user favorites alone when a request fails after sign-out', async () => {
    const server = heldServer();
    port = server.port;
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });

    await act(async () => {
      result.current.toggle(tapped);
      await settle();
    });
    const nextUser: FavoritesResponse = {
      items: [{ kind: 'track', key: tapped.favorite_key, title: 'Track One' }],
      total: 1,
    };
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.favorites, nextUser);
    await act(async () => {
      server.held[0]!.fail();
      await settle();
    });

    expect(server.held).toEqual([]);
    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual(nextUser);
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('sends the toggle back when the first request timed out after the server applied it', async () => {
    const server = heldServer();
    port = server.port;
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });

    await act(async () => {
      result.current.toggle(tapped);
      result.current.toggle(tapped);
      await settle();
    });
    await act(async () => {
      await server.held[0]!.timeOutAfterApplying();
      await settle();
    });
    expect(server.held.map((r) => r.method)).toEqual(['DELETE']);
    await deliverNewestFirst(server);

    expect(await server.isSaved()).toBe(false);
    expect(result.current.isFavorite(tapped)).toBe(false);
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('keeps the server and the cache on the last tap when the button unmounts mid-request', async () => {
    const server = heldServer();
    port = server.port;
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const { result, unmount } = renderHook(() => useFavorites(), {
      wrapper: wrapperFor(queryClient),
    });

    await act(async () => {
      result.current.toggle(tapped);
      result.current.toggle(tapped);
      await settle();
    });
    unmount();
    await deliverNewestFirst(server);

    expect(await server.isSaved()).toBe(false);
    expect(isCached(queryClient.getQueryData(discoveryKeys.favorites))).toBe(false);
  });

  it('stops sending and leaves the next user cache alone when a request succeeds after sign-out', async () => {
    const server = heldServer();
    port = server.port;
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });

    await act(async () => {
      result.current.toggle(tapped);
      result.current.toggle(tapped);
      await settle();
    });
    const nextUser: FavoritesResponse = {
      items: [{ kind: 'track', key: tapped.favorite_key, title: 'Track One' }],
      total: 1,
    };
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.favorites, nextUser);
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
    await deliverNewestFirst(server);

    expect(server.held).toEqual([]);
    expect(await server.isSaved()).toBe(true);
    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual(nextUser);
    expect(invalidateSpy).not.toHaveBeenCalled();
  });
});

function isCached(response: FavoritesResponse | undefined): boolean {
  return response !== undefined && response.items.some((f) => f.key === tapped.favorite_key);
}
